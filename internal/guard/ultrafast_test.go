package guard

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// Yesterday's ultrafast tokens that the trail showed no ultrafast call for
// are reported once per account and model; ones the trail showed aren't, and
// nothing is read without watched regions (add-tier-pricing D10).
func TestUltrafastUnseenReportedOnce(t *testing.T) {
	h := coverageHarness(t)
	withFast(t, h)
	reads := 0
	used := []activity.UltrafastTokens{
		{Account: "111122223333", Model: "us.openai.gpt-6-astra", In: 1200, Out: 40000},
		{Account: "444455556666", Model: "openai.gpt-6.1-sol", In: 10, Out: 20},
	}
	h.g.Fast.Ultrafast = func(_ context.Context, region string, from time.Time) ([]activity.UltrafastTokens, error) {
		reads++
		if region != "us-east-1" || !from.Equal(h.now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)) {
			t.Errorf("read %s from %v", region, from)
		}
		return used, nil
	}
	h.publish(`{"payer_account_id":"` + payer + `","scopes":[{"account_id":"111122223333","ceiling_usd":100,"mode":"enforce"}],"ai":{"defaults":{"mode":"dry-run"}}}`)
	h.pull()

	daily(h) // no watched regions
	if reads != 0 {
		t.Fatalf("read %d times without watched regions", reads)
	}
	h.g.Fast.MantleRegions = []string{"us-east-1"}
	// The trail showed 444455556666's ultrafast calls yesterday.
	yesterday := h.now.UTC().Add(-24 * time.Hour)
	if err := h.g.Fast.Engine.Store.PutMeta(context.Background(), "ultrafast_seen", activity.UltrafastSeen{yesterday.Format("2006-01-02"): {"444455556666|openai.gpt-6.1-sol": true}}); err != nil {
		t.Fatal(err)
	}
	daily(h)
	got := eventsOf(h, schema.KindUltrafastUnseen)
	if len(got) != 1 || !strings.HasPrefix(got[0], "ultrafast_unseen:111122223333|openai.gpt-6-astra:") {
		t.Fatalf("events %v", got)
	}
	if !h.alerts.has("ultrafast use the trail didn't show (1)") || !h.alerts.has("111122223333 calling openai.gpt-6-astra: 1200 input and 40000 output tokens") {
		t.Fatalf("alerts %v", h.alerts.list)
	}
	// The next day the same use shows again; 111122223333 isn't reported
	// twice. 444455556666 is, as nothing marks it seen that day.
	h.now = h.now.Add(24 * time.Hour)
	daily(h)
	n := 0
	for _, e := range eventsOf(h, schema.KindUltrafastUnseen) {
		if strings.HasPrefix(e, "ultrafast_unseen:111122223333|") {
			n++
		}
	}
	if n != 1 || len(eventsOf(h, schema.KindUltrafastUnseen)) != 2 {
		t.Fatalf("the next day: %v", eventsOf(h, schema.KindUltrafastUnseen))
	}
}
