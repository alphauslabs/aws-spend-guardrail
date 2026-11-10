package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah/jennahtest"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

const v1 = `{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":50000}]}`

func newCLI(t *testing.T) (*CLI, *jennahtest.Server, *bytes.Buffer, *bytes.Buffer) {
	f, j := jennahtest.Start(t)
	out, status := &bytes.Buffer{}, &bytes.Buffer{}
	return &CLI{J: j, MSP: "msp_a", Out: out, Status: status}, f, out, status
}

func TestSetupIsIdempotent(t *testing.T) {
	ctx := context.Background()
	c, f, out, _ := newCLI(t)
	if err := c.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.Datasets(); len(got) != 2 || got[0] != "guardrail.msp_a.config" || got[1] != "guardrail.msp_a.inbox" {
		t.Fatalf("datasets %v", got)
	}
	if !strings.Contains(out.String(), "datastore.data:write") {
		t.Fatalf("permissions not printed: %s", out)
	}
	declares := f.Declares
	out.Reset()
	if err := c.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	if f.Declares != declares || !strings.Contains(out.String(), "nothing changed") {
		t.Fatalf("second run changed something: %s", out)
	}
}

func TestSetupRefusesBadMSPID(t *testing.T) {
	c, _, _, _ := newCLI(t)
	c.MSP = "MSP.A"
	if err := c.Setup(context.Background()); err == nil {
		t.Fatal("want error")
	}
}

func TestPublishShowsLooseningAndRefusesInvalid(t *testing.T) {
	ctx := context.Background()
	c, _, out, _ := newCLI(t)
	if err := c.Publish(ctx, []byte(v1), ""); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	raised := strings.Replace(v1, "50000", "60000", 1)
	if err := c.Publish(ctx, []byte(raised), "raise"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ceiling_usd 50000.00 -> 60000.00 (loosening)") || !strings.Contains(out.String(), "after approval") {
		t.Fatalf("loosening not shown:\n%s", out)
	}
	if !strings.Contains(out.String(), "published version 2") {
		t.Fatalf("%s", out)
	}
	bad := `{"payer_account_id":"123456789012","scopes":[{"account_id":"123456789012","ceiling_usd":1}]}`
	if err := c.Publish(ctx, []byte(bad), ""); err == nil || !strings.Contains(err.Error(), "payer account cannot be a scope") {
		t.Fatalf("invalid published: %v", err)
	}
}

func TestPublishRefusesOtherPayer(t *testing.T) {
	ctx := context.Background()
	c, _, _, _ := newCLI(t)
	_ = c.Publish(ctx, []byte(v1), "")
	other := strings.Replace(v1, "123456789012", "999999999999", 1)
	if err := c.Publish(ctx, []byte(other), ""); err == nil {
		t.Fatal("publishing for another payer should fail")
	}
}

func TestRetriedPublishWritesOneVersion(t *testing.T) {
	ctx := context.Background()
	c, f, out, _ := newCLI(t)
	f.LoseCommitResponse = 1
	if err := c.Publish(ctx, []byte(v1), ""); err == nil {
		t.Fatal("expected the lost response to surface")
	}
	out.Reset()
	if err := c.Publish(ctx, []byte(v1), ""); err != nil {
		t.Fatal(err)
	}
	if n := len(f.Rows(schema.ConfigDatasetID("msp_a"), schema.ConfigTable)); n != 1 {
		t.Fatalf("want 1 version, got %d", n)
	}
	if !strings.Contains(out.String(), "already published as version 1") {
		t.Fatalf("%s", out)
	}
}

func TestPulledFileRepublishedIsNoChange(t *testing.T) {
	ctx := context.Background()
	c, _, out, status := newCLI(t)
	_ = c.Publish(ctx, []byte(v1), "")
	out.Reset()
	if err := c.Pull(ctx, 0); err != nil {
		t.Fatal(err)
	}
	pulled := append([]byte(nil), out.Bytes()...)
	var probe map[string]any
	if json.Unmarshal(pulled, &probe) != nil {
		t.Fatalf("pulled document is not clean JSON: %q", pulled)
	}
	if !strings.Contains(status.String(), "version 1") {
		t.Fatalf("status %q", status)
	}
	out.Reset()
	if err := c.Diff(ctx, pulled); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no differences") {
		t.Fatalf("diff of a pulled file: %s", out)
	}
}

func TestPullReportsPending(t *testing.T) {
	ctx := context.Background()
	c, _, out, status := newCLI(t)
	_ = c.Publish(ctx, []byte(v1), "")
	_ = c.J.CommitEvent(ctx, schema.InboxDatasetID("msp_a"), jennah.Event{ID: "config_applied:1", OccurredAt: time.Now(), Kind: schema.KindConfigApplied, ConfigVersion: 1, GuardVersion: "v0"})
	_ = c.Publish(ctx, []byte(strings.Replace(v1, "50000", "60000", 1)), "")
	out.Reset()
	if err := c.Pull(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.String(), "version 1 is applied, version 2 is pending") {
		t.Fatalf("status %q", status)
	}
	if !strings.Contains(out.String(), "60000") {
		t.Fatal("pull should return the newest version")
	}
	out.Reset()
	if err := c.Pull(ctx, 1); err != nil || !strings.Contains(out.String(), "50000") {
		t.Fatalf("pull --version 1: %v %s", err, out)
	}
	if err := c.Pull(ctx, 9); err == nil {
		t.Fatal("pulling a missing version should fail")
	}
}

func TestDiffAgainstAppliedVersion(t *testing.T) {
	ctx := context.Background()
	c, _, out, _ := newCLI(t)
	_ = c.Publish(ctx, []byte(v1), "")
	_ = c.J.CommitEvent(ctx, schema.InboxDatasetID("msp_a"), jennah.Event{ID: "config_applied:1", OccurredAt: time.Now(), Kind: schema.KindConfigApplied, ConfigVersion: 1, GuardVersion: "v0"})
	_ = c.Publish(ctx, []byte(strings.Replace(v1, "50000", "60000", 1)), "")
	out.Reset()
	lower := strings.Replace(v1, "50000", "40000", 1)
	if err := c.Diff(ctx, []byte(lower)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "compared with: version 1") || !strings.Contains(out.String(), "only tightens") {
		t.Fatalf("%s", out)
	}
}

func TestValidate(t *testing.T) {
	c, _, out, _ := newCLI(t)
	if err := c.Validate([]byte(v1), "123456789012"); err != nil || !strings.Contains(out.String(), "valid") {
		t.Fatal(err)
	}
	if err := c.Validate([]byte(v1), "999999999999"); err == nil {
		t.Fatal("want payer mismatch")
	}
}
