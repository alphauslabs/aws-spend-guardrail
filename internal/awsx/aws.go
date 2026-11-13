package awsx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/guard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

// OrgAPI is the part of the Organizations client the guard uses.
type OrgAPI interface {
	ListAccountsForParent(context.Context, *organizations.ListAccountsForParentInput, ...func(*organizations.Options)) (*organizations.ListAccountsForParentOutput, error)
	ListOrganizationalUnitsForParent(context.Context, *organizations.ListOrganizationalUnitsForParentInput, ...func(*organizations.Options)) (*organizations.ListOrganizationalUnitsForParentOutput, error)
	ListTargetsForPolicy(context.Context, *organizations.ListTargetsForPolicyInput, ...func(*organizations.Options)) (*organizations.ListTargetsForPolicyOutput, error)
	ListAccounts(context.Context, *organizations.ListAccountsInput, ...func(*organizations.Options)) (*organizations.ListAccountsOutput, error)
	ListParents(context.Context, *organizations.ListParentsInput, ...func(*organizations.Options)) (*organizations.ListParentsOutput, error)
}

// ListAccounts lists every account in the organization with its state.
func (o *Org) ListAccounts(ctx context.Context) ([]guard.Account, error) {
	var out []guard.Account
	var token *string
	for {
		p, err := o.API.ListAccounts(ctx, &organizations.ListAccountsInput{NextToken: token})
		if err != nil {
			return nil, err
		}
		for _, a := range p.Accounts {
			state := string(a.State)
			if state == "" {
				state = string(a.Status) // older field, same meaning for ACTIVE and SUSPENDED
			}
			out = append(out, guard.Account{ID: aws.ToString(a.Id), Name: aws.ToString(a.Name), State: state})
		}
		if p.NextToken == nil {
			return out, nil
		}
		token = p.NextToken
	}
}

// ParentOf names an account's parent: "root" or the OU id.
func (o *Org) ParentOf(ctx context.Context, account string) (string, error) {
	p, err := o.API.ListParents(ctx, &organizations.ListParentsInput{ChildId: aws.String(account)})
	if err != nil {
		return "", err
	}
	if len(p.Parents) == 0 {
		return "", errors.New("no parent")
	}
	if string(p.Parents[0].Type) == "ROOT" {
		return "root", nil
	}
	return aws.ToString(p.Parents[0].Id), nil
}

// Org implements guard.Org.
type Org struct{ API OrgAPI }

// AccountsInOU lists the active accounts in an OU and all OUs below it.
func (o *Org) AccountsInOU(ctx context.Context, ouID string) ([]string, error) {
	var out []string
	queue := []string{ouID}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		var token *string
		for {
			p, err := o.API.ListAccountsForParent(ctx, &organizations.ListAccountsForParentInput{ParentId: aws.String(parent), NextToken: token})
			if err != nil {
				return nil, err
			}
			for _, a := range p.Accounts {
				if a.State == "" || string(a.State) == "ACTIVE" {
					out = append(out, aws.ToString(a.Id))
				}
			}
			if p.NextToken == nil {
				break
			}
			token = p.NextToken
		}
		token = nil
		for {
			p, err := o.API.ListOrganizationalUnitsForParent(ctx, &organizations.ListOrganizationalUnitsForParentInput{ParentId: aws.String(parent), NextToken: token})
			if err != nil {
				return nil, err
			}
			for _, ou := range p.OrganizationalUnits {
				queue = append(queue, aws.ToString(ou.Id))
			}
			if p.NextToken == nil {
				break
			}
			token = p.NextToken
		}
	}
	return out, nil
}

// PolicyTargets lists where a policy is attached.
func (o *Org) PolicyTargets(ctx context.Context, policyID string) ([]config.Target, error) {
	var out []config.Target
	var token *string
	for {
		p, err := o.API.ListTargetsForPolicy(ctx, &organizations.ListTargetsForPolicyInput{PolicyId: aws.String(policyID), NextToken: token})
		if err != nil {
			return nil, err
		}
		for _, t := range p.Targets {
			kind := "account"
			switch string(t.Type) {
			case "ORGANIZATIONAL_UNIT":
				kind = "ou"
			case "ROOT":
				kind = "root"
			}
			out = append(out, config.Target{Kind: kind, ID: aws.ToString(t.TargetId)})
		}
		if p.NextToken == nil {
			return out, nil
		}
		token = p.NextToken
	}
}

// DynamoAPI is the part of the DynamoDB client the state store uses.
type DynamoAPI interface {
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(context.Context, *dynamodb.DeleteItemInput, ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

// State implements guard.StateStore on one DynamoDB table with a string
// partition key "pk" and sort key "sk". The config state is one item; each
// frozen scope is its own item, so the notification handler and the scheduled
// pull never write the same item.
type State struct {
	API   DynamoAPI
	Table string
}

const (
	pkState    = "state"
	pkFrozen   = "frozen"
	pkReported = "reported"
)

func key(pk, sk string) map[string]ddbtypes.AttributeValue {
	return map[string]ddbtypes.AttributeValue{
		"pk": &ddbtypes.AttributeValueMemberS{Value: pk},
		"sk": &ddbtypes.AttributeValueMemberS{Value: sk},
	}
}

func (s *State) get(ctx context.Context, pk, sk string, v any) (bool, error) {
	out, err := s.API.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(s.Table), Key: key(pk, sk), ConsistentRead: aws.Bool(true)})
	if err != nil {
		return false, err
	}
	doc, ok := out.Item["doc"].(*ddbtypes.AttributeValueMemberS)
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal([]byte(doc.Value), v)
}

func (s *State) put(ctx context.Context, pk, sk string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	item := key(pk, sk)
	item["doc"] = &ddbtypes.AttributeValueMemberS{Value: string(b)}
	_, err = s.API.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.Table), Item: item})
	return err
}

// Load reads the config state, or an empty one on first run.
func (s *State) Load(ctx context.Context) (*guard.State, error) {
	st := &guard.State{}
	if _, err := s.get(ctx, pkState, "config", st); err != nil {
		return nil, err
	}
	return st, nil
}

// Save writes the config state.
func (s *State) Save(ctx context.Context, st *guard.State) error {
	return s.put(ctx, pkState, "config", st)
}

// MarkFrozen records a freeze.
func (s *State) MarkFrozen(ctx context.Context, f guard.Frozen) error {
	return s.put(ctx, pkFrozen, f.Target.String(), f)
}

// ClearFrozen removes a freeze record.
func (s *State) ClearFrozen(ctx context.Context, t config.Target) error {
	_, err := s.API.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: aws.String(s.Table), Key: key(pkFrozen, t.String())})
	return err
}

// ListFrozen lists freeze records.
func (s *State) ListFrozen(ctx context.Context) ([]guard.Frozen, error) {
	var out []guard.Frozen
	var start map[string]ddbtypes.AttributeValue
	for {
		page, err := s.API.Query(ctx, &dynamodb.QueryInput{
			TableName:                 aws.String(s.Table),
			KeyConditionExpression:    aws.String("pk = :pk"),
			ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{":pk": &ddbtypes.AttributeValueMemberS{Value: pkFrozen}},
			ConsistentRead:            aws.Bool(true),
			ExclusiveStartKey:         start,
		})
		if err != nil {
			return nil, err
		}
		for _, it := range page.Items {
			doc, ok := it["doc"].(*ddbtypes.AttributeValueMemberS)
			if !ok {
				continue
			}
			var f guard.Frozen
			if err := json.Unmarshal([]byte(doc.Value), &f); err != nil {
				return nil, fmt.Errorf("frozen record: %w", err)
			}
			out = append(out, f)
		}
		if len(page.LastEvaluatedKey) == 0 {
			return out, nil
		}
		start = page.LastEvaluatedKey
	}
}

// Reported returns the accounts already reported for a kind.
func (s *State) Reported(ctx context.Context, kind string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	var start map[string]ddbtypes.AttributeValue
	for {
		page, err := s.API.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(s.Table),
			KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :k)"),
			ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
				":pk": &ddbtypes.AttributeValueMemberS{Value: pkReported},
				":k":  &ddbtypes.AttributeValueMemberS{Value: kind + "#"},
			},
			ConsistentRead:    aws.Bool(true),
			ExclusiveStartKey: start,
		})
		if err != nil {
			return nil, err
		}
		for _, it := range page.Items {
			sk, _ := it["sk"].(*ddbtypes.AttributeValueMemberS)
			doc, _ := it["doc"].(*ddbtypes.AttributeValueMemberS)
			if sk == nil {
				continue
			}
			var at time.Time
			if doc != nil {
				_ = json.Unmarshal([]byte(doc.Value), &at)
			}
			out[strings.TrimPrefix(sk.Value, kind+"#")] = at
		}
		if len(page.LastEvaluatedKey) == 0 {
			return out, nil
		}
		start = page.LastEvaluatedKey
	}
}

// MarkReported records that an account was reported for a kind.
func (s *State) MarkReported(ctx context.Context, kind, account string, at time.Time) error {
	return s.put(ctx, pkReported, kind+"#"+account, at)
}

// HeartbeatDigest reads the digest of the state the last heartbeat carried, or
// "" when none was written yet.
func (s *State) HeartbeatDigest(ctx context.Context) (string, error) {
	var d string
	_, err := s.get(ctx, pkState, "heartbeat_digest", &d)
	return d, err
}

// SetHeartbeatDigest records the digest of the state a heartbeat carried.
func (s *State) SetHeartbeatDigest(ctx context.Context, digest string) error {
	return s.put(ctx, pkState, "heartbeat_digest", digest)
}

// ClearReported forgets a report, so a later lapse is reported again.
func (s *State) ClearReported(ctx context.Context, kind, account string) error {
	_, err := s.API.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: aws.String(s.Table), Key: key(pkReported, kind+"#"+account)})
	return err
}

// SNSAPI is the part of the SNS client the alerter uses.
type SNSAPI interface {
	Publish(context.Context, *sns.PublishInput, ...func(*sns.Options)) (*sns.PublishOutput, error)
}

// Alerts implements guard.Alerter on the stack's local alert topic.
type Alerts struct {
	API      SNSAPI
	TopicARN string
	Prefix   string // added to every subject, e.g. the payer account
}

// Alert publishes one message. SNS subjects are limited to 100 characters.
func (a *Alerts) Alert(ctx context.Context, subject, msg string) error {
	if a.Prefix != "" {
		subject = a.Prefix + " " + subject
	}
	if len(subject) > 100 {
		subject = subject[:100]
	}
	_, err := a.API.Publish(ctx, &sns.PublishInput{TopicArn: aws.String(a.TopicARN), Subject: aws.String(subject), Message: aws.String(msg)})
	return err
}

// BudgetNameFromNotification finds the budget name in a Budgets SNS message.
// Budgets notifications are plain text with a "Budget Name: <name>" line;
// budget action notifications are JSON with a "budgetName" field. Both are
// handled, and the exact formats are confirmed in task 1.1.
func BudgetNameFromNotification(msg string) (string, error) {
	var j map[string]any
	if json.Unmarshal([]byte(msg), &j) == nil {
		for _, k := range []string{"budgetName", "BudgetName", "budget_name"} {
			if v, ok := j[k].(string); ok && v != "" {
				return v, nil
			}
		}
	}
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "Budget Name:"); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", errors.New("no budget name in notification")
}

var (
	_ guard.Org        = (*Org)(nil)
	_ guard.StateStore = (*State)(nil)
	_ guard.Alerter    = (*Alerts)(nil)
)
