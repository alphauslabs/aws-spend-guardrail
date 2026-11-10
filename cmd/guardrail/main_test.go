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
