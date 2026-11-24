package activity_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity/activitytest"
)

// tierPack records the tier each ConverseStream completion reaches it with,
// and flags calls the way the AI pack does: model "turbo.model" as on an
// unpriced tier, and calls with no tokens as unmeasured.
type tierPack struct{ seen *[]string }

func (tierPack) Wants(r activity.Record) bool { return r.EventSource == "bedrock.amazonaws.com" }
func (p tierPack) Evaluate(r activity.Record) (*activity.Usage, *activity.Hit) {
	if r.EventType == "AwsServiceEvent" {
		tier := r.ServiceTier
		if r.TierUnknown {
			tier = "?"
		}
		*p.seen = append(*p.seen, r.ParentRequestID+"="+tier)
	}
	u := &activity.Usage{Account: r.Account, Time: r.EventTime, Model: r.ModelID, Calls: 1, In: r.InputTokens, Out: r.OutputTokens}
	if r.ModelID == "turbo.model" {
		u.UnpricedTier = "turbo"
	}
	if r.InputTokens == 0 && r.OutputTokens == 0 {
		u.Unmeasured = true
	}
	return u, nil
}

func streamCall(account, requestID, tier string, t time.Time) map[string]any {
	req := map[string]any{"modelId": "openai.gpt-oss-20b-1:0"}
	if tier != "" {
		req["serviceTier"] = map[string]any{"type": tier}
	}
	return map[string]any{
		"eventTime": t.UTC().Format(time.RFC3339), "eventSource": "bedrock.amazonaws.com", "eventName": "ConverseStream",
		"eventType": "AwsApiCall", "awsRegion": "us-east-1", "recipientAccountId": account, "requestID": requestID,
		"userIdentity": map[string]any{"type": "AssumedRole", "arn": "arn:aws:sts::" + account + ":assumed-role/app/x"}, "requestParameters": req,
	}
}

func streamDone(account, parent string, t time.Time) map[string]any {
	return map[string]any{
		"eventTime": t.UTC().Format(time.RFC3339), "eventSource": "bedrock.amazonaws.com", "eventName": "ConverseStream",
		"eventType": "AwsServiceEvent", "awsRegion": "us-east-1", "recipientAccountId": account,
		"userIdentity": map[string]any{"invokedBy": "bedrock.amazonaws.com"},
		"serviceEventDetails": map[string]any{"parentRequestId": parent,
			"AdditionalEventData": map[string]any{"additionalEntries": map[string]any{"inputTokens": 69, "outputTokens": 5}}},
	}
}

func (w *world) file(records ...map[string]any) {
	w.n++
	w.bucket.Put(activitytest.TrailKey(base, acct1, "us-east-1", w.now.Add(-time.Minute), string(rune('a'+w.n))), activitytest.TrailFile(records...), w.now)
}

// A ConverseStream completion takes its call record's tier, from the same
// tick or one within the window; one whose call record wasn't seen is tier
// unknown (add-tier-pricing D7).
func TestStreamCompletionTakesItsCallsTier(t *testing.T) {
	w := newWorld(t)
	var seen []string
	p := tierPack{seen: &seen}
	callAt := w.now.Add(-time.Minute)
	// Same file, completion first: the call record still applies.
	w.file(streamDone(acct1, "r1", callAt), streamCall(acct1, "r1", "priority", callAt))
	// A standard stream.
	w.file(streamCall(acct1, "r2", "", callAt), streamDone(acct1, "r2", callAt))
	// A call record whose completion comes in a later tick.
	w.file(streamCall(acct1, "r3", "flex", callAt))
	if err := w.tickWith(p); err != nil {
		t.Fatal(err)
	}
	w.now = w.now.Add(5 * time.Minute)
	w.file(streamDone(acct1, "r3", callAt), streamDone(acct1, "r9", callAt))
	if err := w.tickWith(p); err != nil {
		t.Fatal(err)
	}
	// Past the window, the call record is forgotten.
	w.file(streamCall(acct1, "r4", "priority", w.now.Add(-time.Minute)))
	if err := w.tickWith(p); err != nil {
		t.Fatal(err)
	}
	w.now = w.now.Add(activity.StreamTierWindow + time.Minute)
	w.file(streamDone(acct1, "r4", w.now.Add(-time.Minute)))
	if err := w.tickWith(p); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(seen, " "), "r1=priority r2= r3=flex r9=? r4=?"; got != want {
		t.Fatalf("tiers %q, want %q", got, want)
	}
}

// quiet writes one call to quiet.model with no token counts.
func (w *world) quiet(account string) {
	w.n++
	w.bucket.Put(activitytest.TrailKey(base, account, "us-east-1", w.now.Add(-time.Minute), string(rune('a'+w.n))),
		activitytest.TrailFile(activitytest.BedrockCall(account, "us-east-1", "quiet.model", "arn:aws:sts::"+account+":assumed-role/app/x", w.now.Add(-time.Minute), 0, 0, false)), w.now)
}

// An unpriced tier is reported once per model and tier, and an unmeasured
// call once per account and model (add-tier-pricing D5, D9).
func TestTierReportsOnce(t *testing.T) {
	w := newWorld(t)
	var seen []string
	p := tierPack{seen: &seen}
	for i := 0; i < 5; i++ {
		w.call(acct1, "us-east-1", "turbo.model", w.now.Add(-time.Minute), 10)
		w.quiet([]string{acct1, acct2}[i%2])
	}
	if err := w.tickWith(p); err != nil {
		t.Fatal(err)
	}
	w.now = w.now.Add(time.Minute)
	w.call(acct2, "us-east-1", "turbo.model", w.now.Add(-time.Minute), 10)
	w.quiet(acct1)
	if err := w.tickWith(p); err != nil {
		t.Fatal(err)
	}
	if n := w.count("unpriced_tier unpriced_tier:ai:turbo.model:turbo"); n != 1 {
		t.Fatalf("unpriced tier events %d: %v", n, w.events)
	}
	if n := w.count("unmeasured_call unmeasured_call:ai:"); n != 2 {
		t.Fatalf("unmeasured events %d (one per account): %v", n, w.events)
	}
	alerts := strings.Join(w.alerts, "|")
	if strings.Count(alerts, "turbo tier of turbo.model has no price") != 1 || strings.Count(alerts, "calls to quiet.model can't be measured") != 2 {
		t.Fatalf("alerts %q", alerts)
	}
}

// The trail's ultrafast calls are kept per day for the daily check, and days
// before yesterday dropped (add-tier-pricing D10).
func TestUltrafastCallsKeptPerDay(t *testing.T) {
	w := newWorld(t)
	var seen []string
	call := activitytest.BedrockCall(acct1, "us-east-1", "us.openai.gpt-6-astra", "arn:aws:sts::"+acct1+":assumed-role/app/x", w.now.Add(-time.Minute), 20, 16, false)
	call["additionalEventData"].(map[string]any)["serviceTier"] = "ultrafast"
	w.file(call)
	if err := w.tickWith(tierPack{seen: &seen}); err != nil {
		t.Fatal(err)
	}
	got, err := activity.UltrafastSeenOn(context.Background(), w.store, w.now)
	if err != nil || !got[acct1+"|us.openai.gpt-6-astra"] || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	w.now = w.now.Add(72 * time.Hour)
	if err := w.tickWith(tierPack{seen: &seen}); err != nil {
		t.Fatal(err)
	}
	if got, _ := activity.UltrafastSeenOn(context.Background(), w.store, w.now.Add(-72*time.Hour)); len(got) != 0 {
		t.Fatalf("an old day kept: %v", got)
	}
}
