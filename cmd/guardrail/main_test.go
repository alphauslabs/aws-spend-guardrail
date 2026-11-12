package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestInstallLinkPrintsOnlyTheURL(t *testing.T) {
	// No credentials or endpoint are set; the command must not need them.
	t.Setenv("JENNAH_API_KEY", "")
	t.Setenv("AWS_PROFILE", "does-not-exist")
	var out, errb bytes.Buffer
	err := run(context.Background(), []string{"install-link",
		"--msp", "msp_a", "--payer", "123456789012", "--bucket", "bkt-1", "--prefix", "guard/v0.1.0",
		"--version", "v0.1.0", "--approvers", "a@example.com, b@example.com"}, &out, &errb)
	if err != nil {
		t.Fatalf("%v (stderr %q)", err, errb.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "https://us-east-1.console.aws.amazon.com/cloudformation/") {
		t.Fatalf("stdout should be exactly the URL, got %q", out.String())
	}
	if !strings.Contains(lines[0], "param_LooseningApprovers=a%40example.com%2Cb%40example.com") {
		t.Fatalf("approvers not trimmed and joined: %s", lines[0])
	}
}

func TestInstallLinkRefusesAKey(t *testing.T) {
	var out, errb bytes.Buffer
	err := run(context.Background(), []string{"install-link",
		"--msp", "msp_a", "--payer", "123456789012", "--bucket", "bkt-1", "--prefix", "guard/v0.1.0",
		"--reader-secret-arn", "jennah_sk_0123456789abcdef"}, &out, &errb)
	if err == nil || out.Len() != 0 {
		t.Fatalf("want a refusal and no output, got err=%v out=%q", err, out.String())
	}
}

func TestPreflightBudgetActionRoom(t *testing.T) {
	var out strings.Builder
	if err := checkBudgetActions(5, 60, &out); err != nil {
		t.Fatalf("5 + 60 should pass: %v", err)
	}
	if !strings.Contains(out.String(), "budget actions: 5 outside the guard, 60 enforcing scopes in this config, limit 100") {
		t.Fatalf("output = %q", out.String())
	}
	out.Reset()
	err := checkBudgetActions(30, 80, &out)
	if err == nil || !strings.Contains(err.Error(), "needs 110 budget actions (30 outside the guard and 80 for enforcing scopes), and AWS allows 100") {
		t.Fatalf("30 + 80 should fail naming the counts: %v", err)
	}
	out.Reset()
	if err := checkBudgetActions(0, 1, &out); err != nil || !strings.Contains(out.String(), "1 enforcing scope in this config") {
		t.Fatalf("one scope: %q %v", out.String(), err)
	}
	if err := checkBudgetActions(0, 100, &out); err != nil {
		t.Fatalf("exactly 100 should pass: %v", err)
	}
}
