// Command guard is the guard Lambda function. One function handles every
// trigger: the 5-minute schedule (pull config and reconcile), the daily
// schedule (period lifts, drift, heartbeat), budget notifications (record a
// freeze or would-freeze), and the outbox queue (write events to Jennah).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/awsx"
	"github.com/alphauslabs/aws-spend-guardrail/internal/guard"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/outbox"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
	"github.com/aws/aws-lambda-go/cfn"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	lambdasvc "github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

type app struct {
	g        *guard.Guard
	consumer *outbox.Consumer
}

func env(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("missing environment variable %s", name)
	}
	return v
}

func envDuration(name string) time.Duration {
	d, err := time.ParseDuration(env(name))
	if err != nil {
		log.Fatalf("%s: %v", name, err)
	}
	return d
}

func secret(ctx context.Context, sm *secretsmanager.Client, arn string) string {
	out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(arn)})
	if err != nil {
		log.Fatalf("read secret %s: %v", arn, err)
	}
	return strings.TrimSpace(aws.ToString(out.SecretString))
}

func setup(ctx context.Context) *app {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		log.Fatalf("aws config: %v", err)
	}
	sm := secretsmanager.NewFromConfig(cfg)
	endpoint := os.Getenv("JENNAH_ENDPOINT") // empty: the SDK's default public endpoint
	reader, err := jennah.Dial(endpoint, secret(ctx, sm, env("READER_SECRET_ARN")))
	if err != nil {
		log.Fatalf("jennah reader: %v", err)
	}
	writer, err := jennah.Dial(endpoint, secret(ctx, sm, env("WRITER_SECRET_ARN")))
	if err != nil {
		log.Fatalf("jennah writer: %v", err)
	}
	required, _ := strconv.Atoi(env("LOOSENING_REQUIRED_APPROVALS"))
	payer := env("PAYER_ACCOUNT_ID")
	mspID := env("MSP_ID")
	orgs := organizations.NewFromConfig(cfg)
	snsClient := sns.NewFromConfig(cfg)
	s := guard.Settings{
		PayerAccountID:     payer,
		MSPID:              mspID,
		GuardVersion:       version,
		PolicyA:            env("FREEZE_POLICY_A"),
		PolicyB:            env("FREEZE_POLICY_B"),
		LooseningApprovers: strings.Split(env("LOOSENING_APPROVERS"), ","),
		LooseningRequired:  required,
		LooseningWindow:    envDuration("LOOSENING_APPROVAL_WINDOW"),
		LooseningDelay:     envDuration("LOOSENING_DELAY"),
		AlertEmail:         os.Getenv("ALERT_EMAIL"), // optional
	}
	a := &app{
		g: &guard.Guard{
			S:      s,
			Config: reader,
			Events: &outbox.Producer{SQS: sqs.NewFromConfig(cfg), QueueURL: env("OUTBOX_QUEUE_URL")},
			Alerts: &awsx.Alerts{API: snsClient, TopicARN: env("ALERT_TOPIC_ARN"), Prefix: "[" + payer + "]"},
			State:  &awsx.State{API: dynamodb.NewFromConfig(cfg), Table: env("STATE_TABLE")},
			Budgets: &awsx.Budgets{
				API:              budgets.NewFromConfig(cfg),
				Org:              orgs,
				AccountID:        payer,
				ExecutionRoleARN: env("BUDGETS_EXECUTION_ROLE_ARN"),
				NotifyTopicARN:   env("NOTIFY_TOPIC_ARN"),
			},
			Org:  &awsx.Org{API: orgs},
			Subs: &awsx.AlertSubscriptions{API: snsClient, TopicARN: env("ALERT_TOPIC_ARN")},
		},
		consumer: &outbox.Consumer{Jennah: writer, DatasetID: schema.InboxDatasetID(mspID, payer)},
	}
	hash, err := awsx.CodeSHA256(ctx, lambdasvc.NewFromConfig(cfg), os.Getenv("AWS_LAMBDA_FUNCTION_NAME"))
	a.g.SetCodeHash(ctx, hash, err)
	return a
}

type probe struct {
	Task        string `json:"task"`
	RequestType string `json:"RequestType"` // CloudFormation custom resource
	Source      string `json:"source"`      // EventBridge
	Records     []struct {
		EventSource  string `json:"eventSource"`
		EventSource2 string `json:"EventSource"`
	} `json:"Records"`
}

// Kinds of invocation the function handles.
const (
	routeTeardown = "teardown"
	routePull     = "pull"
	routeDaily    = "daily"
	routeOutbox   = "outbox"
	routeNotify   = "notification"
	routeCoverage = "coverage"
)

// route names what an incoming event is, without acting on it.
func route(raw json.RawMessage) (string, error) {
	var p probe
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("unrecognized event: %w", err)
	}
	switch {
	case p.RequestType != "":
		return routeTeardown, nil
	case p.Task == "pull":
		return routePull, nil
	case p.Task == "daily":
		return routeDaily, nil
	case p.Source == "aws.organizations":
		return routeCoverage, nil // an account created, invited or moved
	case len(p.Records) > 0 && p.Records[0].EventSource == "aws:sqs":
		return routeOutbox, nil
	case len(p.Records) > 0 && p.Records[0].EventSource2 == "aws:sns":
		return routeNotify, nil
	}
	return "", fmt.Errorf("unrecognized event: %.200s", raw)
}

func (a *app) handle(ctx context.Context, raw json.RawMessage) (any, error) {
	r, err := route(raw)
	if err != nil {
		return nil, err
	}
	switch r {
	case routeTeardown:
		var ev cfn.Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil, err
		}
		cfn.LambdaWrap(func(ctx context.Context, ev cfn.Event) (string, map[string]any, error) {
			if ev.RequestType == cfn.RequestDelete {
				return "guardrail-teardown", nil, a.g.Teardown(ctx)
			}
			return "guardrail-teardown", nil, nil
		})(ctx, ev)
		return nil, nil
	case routePull:
		return nil, a.g.Pull(ctx)
	case routeDaily:
		return nil, a.g.Daily(ctx)
	case routeCoverage:
		return nil, a.g.CheckCoverage(ctx)
	case routeOutbox:
		var ev events.SQSEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil, err
		}
		return a.consumer.Handle(ctx, ev)
	case routeNotify:
		var ev events.SNSEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil, err
		}
		for _, r := range ev.Records {
			name, err := awsx.BudgetNameFromNotification(r.SNS.Message)
			if err != nil {
				log.Printf("ignoring notification without a budget name: %v", err)
				continue
			}
			if err := a.g.OnThreshold(ctx, name); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unhandled route %q", r)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-version" {
		fmt.Println(version)
		return
	}
	a := setup(context.Background())
	lambda.Start(a.handle)
}
