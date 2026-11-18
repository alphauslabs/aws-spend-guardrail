package guard

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// Accounts not sharing their mantle usage in a watched region are reported
// once, again after they're linked and unlinked, and not at all without a
// sink (add-mantle-usage D6).
func TestMantleUnlinkedReportedOnce(t *testing.T) {
	h := coverageHarness(t)
	withFast(t, h)
	sink := "arn:aws:oam:us-east-1:" + payer + ":sink/s1"
	linked := map[string]bool{"111122223333": true}
	h.g.Fast.MantleRegions = []string{"us-east-1"}
	h.g.Fast.MantleLinks = func(_ context.Context, region string) (string, map[string]bool, error) {
		return sink, linked, nil
	}
	h.publish(`{"payer_account_id":"` + payer + `","scopes":[{"account_id":"111122223333","ceiling_usd":100,"mode":"enforce"},{"account_id":"444455556666","ceiling_usd":100,"mode":"enforce"}],"ai":{"defaults":{"mode":"dry-run"}}}`)
	h.pull()
	daily(h)
	got := eventsOf(h, schema.KindMantleUnlinked)
	if len(got) != 1 || !strings.HasPrefix(got[0], "mantle_unlinked:444455556666@us-east-1:") {
		t.Fatalf("events %v", got)
	}
	if !h.alerts.has("1 account(s) not sharing mantle usage") || !h.alerts.has("444455556666 (acme-dev) in us-east-1") {
		t.Fatalf("alerts %v", h.alerts.list)
	}
	h.now = h.now.Add(24 * time.Hour)
	daily(h)
	if n := len(eventsOf(h, schema.KindMantleUnlinked)); n != 1 {
		t.Fatalf("reported again the next day: %d events", n)
	}
	// Linked, then unlinked again: reported again.
	linked["444455556666"] = true
	h.now = h.now.Add(24 * time.Hour)
	daily(h)
	delete(linked, "444455556666")
	h.now = h.now.Add(24 * time.Hour)
	daily(h)
	if n := len(eventsOf(h, schema.KindMantleUnlinked)); n != 2 {
		t.Fatalf("unlinked again: %d events, want 2", n)
	}
	// No sink: nothing reported (the fast path alerts that it can't read).
	sink = ""
	delete(linked, "111122223333")
	h.now = h.now.Add(24 * time.Hour)
	daily(h)
	if n := len(eventsOf(h, schema.KindMantleUnlinked)); n != 2 {
		t.Fatalf("no sink: %d events, want 2", n)
	}
}
