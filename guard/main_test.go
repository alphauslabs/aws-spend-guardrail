package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/internal/alerttext"
	"github.com/alphauslabs/aws-spend-guardrail/internal/guard"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
)

func TestRoute(t *testing.T) {
	cases := map[string]string{
		`{"task":"pull"}`:           routePull,
		`{"task":"daily"}`:          routeDaily,
		`{"task":"trail"}`:          routeTrail,
		`{"task":"publish_prices"}`: routePrices,
		`{"RequestType":"Delete","ResponseURL":"https://example.com"}`:  routeTeardown,
		`{"Records":[{"eventSource":"aws:sqs","body":"{}"}]}`:           routeOutbox,
		`{"Records":[{"EventSource":"aws:sns","Sns":{"Message":"x"}}]}`: routeNotify,
		// An EventBridge event for an account created through CloudTrail.
		`{"version":"0","source":"aws.organizations","detail-type":"AWS API Call via CloudTrail",
		  "detail":{"eventSource":"organizations.amazonaws.com","eventName":"CreateAccount"}}`: routeCoverage,
	}
	for raw, want := range cases {
		got, err := route(json.RawMessage(raw))
		if err != nil || got != want {
			t.Errorf("%s: got %q %v, want %q", raw, got, err, want)
		}
	}
	if _, err := route(json.RawMessage(`{"hello":1}`)); err == nil {
		t.Error("an unknown event must be refused")
	}
}

func TestAINotStartedAlertReadsWell(t *testing.T) {
	if err := alerttext.Check(aiNotStarted(errors.New("find the region of trail bucket b: access denied"))); err != nil {
		t.Fatal(err)
	}
}

// TestGuardRoleGrantsCalls checks that every AWS Organizations and DynamoDB
// call the guard's code makes is granted to the guard role, in its own policy
// or the AI guard's. The guard role is the only identity that runs this code,
// so a missing grant shows up only in a live run otherwise. CLI-only code
// (cmd/ and the preflight checks) runs with the installer's credentials and is
// left out.
func TestGuardRoleGrantsCalls(t *testing.T) {
	raw, err := os.ReadFile("template.yaml")
	if err != nil {
		t.Fatal(err)
	}
	tpl := string(raw)
	start := strings.Index(tpl, "\n  GuardRole:\n")
	end := strings.Index(tpl, "\n  TrailSchedule:\n")
	if start < 0 || end < start {
		t.Fatal("can't find the GuardRole and AIGuardPolicy resources in template.yaml")
	}
	grants := tpl[start:end]

	call := regexp.MustCompile(`\b(organizations|dynamodb|pricing|bedrock)\.([A-Z][A-Za-z]+)Input\b`)
	cliOnly := map[string]bool{filepath.Join("..", "internal", "awsx", "preflight.go"): true}
	calls := map[string]string{}
	for _, root := range []string{".", filepath.Join("..", "internal")} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || cliOnly[path] {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range call.FindAllStringSubmatch(string(src), -1) {
				calls[m[1]+":"+m[2]] = path
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"organizations:DescribeOrganization", "organizations:AttachPolicy", "dynamodb:UpdateItem", "dynamodb:GetItem", "pricing:GetProducts", "pricing:GetAttributeValues", "bedrock:ListFoundationModels"} {
		if calls[want] == "" {
			t.Fatalf("expected to find the guard's call %s, found %v", want, calls)
		}
	}
	// The mantle model list is a signed HTTP request, not an SDK call
	// (internal/prices/sources.go), so it is checked by name.
	if !strings.Contains(grants, "bedrock-mantle:ListModels") {
		t.Error("internal/prices/sources.go lists mantle models, which needs bedrock-mantle:ListModels in template.yaml")
	}
	for action, path := range calls {
		if !strings.Contains(grants, action) {
			t.Errorf("%s calls %s, which the guard role isn't granted in template.yaml", path, action)
		}
	}
}

// The template's TEMPLATE_REVISION must equal the code's MinTemplateRevision,
// so a release always ships them matched (add-guard-upgrades D2).
func TestTemplateRevisionMatchesCode(t *testing.T) {
	raw, err := os.ReadFile("template.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\s+TEMPLATE_REVISION: "(\d+)"$`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal(`template.yaml has no TEMPLATE_REVISION: "N" in the function's environment`)
	}
	if n, _ := strconv.Atoi(m[1]); n != guard.MinTemplateRevision {
		t.Fatalf("template revision %d, code needs %d: bump both together", n, guard.MinTemplateRevision)
	}
}

func TestTemplateRevisionFromEnvironment(t *testing.T) {
	for v, want := range map[string]int{"": 0, "3": 3, "x": 0, "-1": 0} {
		t.Setenv("TEMPLATE_REVISION", v)
		if got := templateRevision(); got != want {
			t.Errorf("%q: %d, want %d", v, got, want)
		}
	}
}

// The price publisher key is dialed only when checked; a failure to read it
// is the check's error, not a crash (add-scheduled-price-publish D4, D6).
func TestLazyKeyDialsOnUse(t *testing.T) {
	dialed := 0
	k := lazyKey{dial: func(context.Context) (*jennah.Client, error) {
		dialed++
		return nil, errors.New("read the price publisher secret: AccessDenied")
	}}
	if dialed != 0 {
		t.Fatal("dialed before use")
	}
	if _, err := k.CallingKey(context.Background()); err == nil || !strings.Contains(err.Error(), "AccessDenied") || dialed != 1 {
		t.Fatalf("err %v, dialed %d", err, dialed)
	}
}

// The AI cutoff denies exactly these actions (widen-ai-cutoff D1, D4), so a
// change to what a cutoff blocks is deliberate, and the policy stays under the
// 5,120-character limit for a service control policy.
func TestAICutoffActions(t *testing.T) {
	raw, err := os.ReadFile("template.yaml")
	if err != nil {
		t.Fatal(err)
	}
	tpl := string(raw)
	start := strings.Index(tpl, "\n  AIDenyPolicy:\n")
	end := strings.Index(tpl, "\n  # Freeze policies")
	if start < 0 || end < start {
		t.Fatal("can't find AIDenyPolicy in template.yaml")
	}
	var got []string
	for _, l := range strings.Split(tpl[start:end], "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "- bedrock") {
			got = append(got, strings.TrimPrefix(l, "- "))
		}
	}
	want := []string{
		"bedrock:InvokeModel", "bedrock:InvokeModelWithResponseStream", "bedrock:CallWithBearerToken",
		"bedrock:Rerank", "bedrock:Retrieve", "bedrock:RetrieveAndGenerate", "bedrock:AgenticRetrieveStream",
		"bedrock:InvokeAgent", "bedrock:InvokeInlineAgent", "bedrock:InvokeFlow",
		"bedrock:CreateModelInvocationJob", "bedrock:CreateProvisionedModelThroughput", "bedrock:CreateModelCustomizationJob",
		"bedrock:InvokeDataAutomation", "bedrock:InvokeDataAutomationAsync",
		"bedrock-mantle:CreateInference", "bedrock-mantle:CallWithBearerToken", "bedrock-mantle:CreateFineTuningJob", "bedrock-mantle:CreateReservation",
		"bedrock-agentcore:Invoke*", "bedrock-agentcore:Start*",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("AI cutoff actions:\n got %v\nwant %v", got, want)
	}
	// The policy as AWS stores it: compact JSON of the statement.
	policy, _ := json.Marshal(map[string]any{"Version": "2012-10-17", "Statement": []map[string]any{{
		"Sid": "GuardrailAICutoff", "Effect": "Deny", "Action": got, "Resource": "*"}}})
	if len(policy) >= 5120 {
		t.Fatalf("the AI cutoff policy is %d characters; service control policies allow 5,120", len(policy))
	}
}
