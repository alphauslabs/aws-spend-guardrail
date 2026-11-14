package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/alphauslabs/aws-spend-guardrail/internal/alerttext"
)

func TestRoute(t *testing.T) {
	cases := map[string]string{
		`{"task":"pull"}`:  routePull,
		`{"task":"daily"}`: routeDaily,
		`{"task":"trail"}`: routeTrail,
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

	call := regexp.MustCompile(`\b(organizations|dynamodb)\.([A-Z][A-Za-z]+)Input\b`)
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
	for _, want := range []string{"organizations:DescribeOrganization", "organizations:AttachPolicy", "dynamodb:UpdateItem", "dynamodb:GetItem"} {
		if calls[want] == "" {
			t.Fatalf("expected to find the guard's call %s, found %v", want, calls)
		}
	}
	for action, path := range calls {
		if !strings.Contains(grants, action) {
			t.Errorf("%s calls %s, which the guard role isn't granted in template.yaml", path, action)
		}
	}
}
