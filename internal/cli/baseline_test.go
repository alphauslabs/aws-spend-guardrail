package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// applied reports a version as applied, as the guard's config_applied event.
func applied(t *testing.T, c *CLI, v int64) {
	t.Helper()
	if err := c.J.CommitEvent(context.Background(), schema.InboxDatasetID("msp_a", "123456789012"), jennah.Event{
		ID: "config_applied:" + string(rune('0'+v)), OccurredAt: time.Now(), Kind: schema.KindConfigApplied, ConfigVersion: v, GuardVersion: "v0"}); err != nil {
		t.Fatal(err)
	}
}

func diffOut(t *testing.T, c *CLI, out interface {
	String() string
	Reset()
}, doc string) string {
	t.Helper()
	out.Reset()
	if err := c.Diff(context.Background(), []byte(doc)); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// The CLI compares with what the guard will hold once it has read the newest
// published version (fix-diff-baseline D1, D2).
func TestDiffBaseline(t *testing.T) {
	ctx := context.Background()
	ceiling := func(n string) string { return strings.Replace(v1, "50000", n, 1) }

	// No applied version reported yet: the newest.
	c, _, out, _ := newCLI(t)
	_ = c.Publish(ctx, []byte(v1), "")
	if got := diffOut(t, c, out, ceiling("40000")); !strings.Contains(got, "compared with: version 1 (no applied version reported yet)") {
		t.Errorf("no report: %s", got)
	}

	// The newest is the applied version.
	applied(t, c, 1)
	if got := diffOut(t, c, out, ceiling("40000")); !strings.Contains(got, "compared with: version 1\n") || !strings.Contains(got, "only tightens") {
		t.Errorf("applied newest: %s", got)
	}

	// A tightening version 2 is published, and its report hasn't arrived:
	// compared with version 2, so undoing it is a loosening (the add-ai-blocked
	// live check case).
	_ = c.Publish(ctx, []byte(ceiling("40000")), "")
	got := diffOut(t, c, out, ceiling("45000"))
	if !strings.Contains(got, "compared with: version 2 (published after the applied version 1; it only tightens") || !strings.Contains(got, "loosens protection") {
		t.Errorf("tightening not yet reported: %s", got)
	}

	// A loosening version 3 waits: compared with version 2's config plus
	// version 3's tightening parts.
	applied(t, c, 2)
	two := `{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":40000},{"account_id":"444455556666","ceiling_usd":9000}]}`
	_ = c.Publish(ctx, []byte(two), "") // version 3: adds a scope (tightening) ...
	applied(t, c, 3)
	three := `{"payer_account_id":"123456789012","scopes":[{"account_id":"111122223333","ceiling_usd":60000},{"account_id":"444455556666","ceiling_usd":8000}]}`
	_ = c.Publish(ctx, []byte(three), "") // version 4: raises one ceiling (waits), lowers the other
	got = diffOut(t, c, out, three)
	if !strings.Contains(got, "compared with: version 3 with the tightening parts of version 4, whose loosening waits for approval") {
		t.Fatalf("waiting loosening: %s", got)
	}
	// Against the stricter merge (40000 and 8000), version 4's own document
	// differs only by the raise still waiting.
	if !strings.Contains(got, "111122223333") || strings.Contains(got, "444455556666") {
		t.Errorf("only the waiting raise should differ: %s", got)
	}

	// The newest is invalid (the guard rejects it): the applied version.
	if err := c.J.InsertVersion(ctx, schema.ConfigDatasetID("msp_a", "123456789012"), jennah.Version{Number: 5, Document: []byte(`{"payer_account_id":"999999999999","scopes":[]}`), PublishedAt: time.Now()}, "v5"); err != nil {
		t.Fatal(err)
	}
	if got := diffOut(t, c, out, three); !strings.Contains(got, "compared with: version 3\n") {
		t.Errorf("invalid newest: %s", got)
	}
}
