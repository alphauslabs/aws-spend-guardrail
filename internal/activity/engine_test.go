package activity_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity/activitytest"
	"github.com/alphauslabs/aws-spend-guardrail/internal/alerttext"
)

const base = "AWSLogs/o-test/"
const acct1, acct2 = "111122223333", "444455556666"

// pricePack prices every Bedrock call at $1 per 1,000 input tokens, and trips
// on the model "forbidden".
type pricePack struct{}

func (pricePack) Wants(r activity.Record) bool { return r.EventSource == "bedrock.amazonaws.com" }
func (pricePack) Evaluate(r activity.Record) (*activity.Usage, *activity.Hit) {
	if r.ModelID == "forbidden" {
		return nil, &activity.Hit{Account: r.Account, Time: r.EventTime, Rule: "model_not_allowed", Detail: r.ModelID}
	}
	return &activity.Usage{Account: r.Account, Time: r.EventTime, Model: r.ModelID, Calls: 1, In: r.InputTokens, Out: r.OutputTokens, USD: float64(r.InputTokens) / 1000}, nil
}

type world struct {
	t        *testing.T
	now      time.Time
	bucket   *activitytest.Bucket
	store    *activitytest.Store
	policies *activitytest.Policies
	events   []string
	alerts   []string
	e        *activity.Engine
	caps     map[string]activity.Caps
	lifts    map[string]bool
	n        int
}

func newWorld(t *testing.T) *world {
	w := &world{t: t, now: time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC), bucket: activitytest.NewBucket(),
		store: activitytest.NewStore(), policies: activitytest.NewPolicies(), caps: map[string]activity.Caps{}, lifts: map[string]bool{}}
	w.restart()
	return w
}

// restart builds a fresh engine on the same store and bucket, as a new
// function instance would.
func (w *world) restart() {
	w.e = &activity.Engine{
		Reader: &activity.Reader{Bucket: w.bucket, Store: w.store, Base: base}, Store: w.store, Policies: w.policies,
		Emit: func(_ context.Context, kind, id, _ string, _ map[string]any) {
			w.events = append(w.events, kind+" "+id)
		},
		Alert: func(_ context.Context, s, body string) {
			if err := alerttext.Check(body); err != nil {
				w.t.Errorf("alert %q: %v\n%s", s, err, body)
			}
			w.alerts = append(w.alerts, s)
		},
		Now: func() time.Time { return w.now },
	}
}

func (w *world) runs() []activity.PackRun {
	return []activity.PackRun{{Name: "ai", Pack: pricePack{}, PolicyID: "p-ai", Lifts: w.lifts,
		CapsFor: func(a string) (activity.Caps, bool) { c, ok := w.caps[a]; return c, ok }}}
}

func (w *world) run() {
	w.t.Helper()
	if err := w.e.Tick(context.Background(), w.runs()); err != nil {
		w.t.Fatalf("tick: %v", err)
	}
}

// call writes one file holding one call, made at callAt, landing now.
func (w *world) call(account, region, model string, callAt time.Time, tokens int64) string {
	w.n++
	k := activitytest.TrailKey(base, account, region, callAt, string(rune('a'+w.n)))
	w.bucket.Put(k, activitytest.TrailFile(activitytest.BedrockCall(account, region, model, "arn:aws:sts::"+account+":assumed-role/app/x", callAt, tokens, 10, false)), w.now)
	return k
}

func (w *world) count(prefix string) int {
	n := 0
	for _, e := range w.events {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

func TestReadsEachFileOnceAcrossRestarts(t *testing.T) {
	w := newWorld(t)
	w.caps[acct1] = activity.Caps{Hourly: 100, Enforce: true}
	w.call(acct1, "us-east-1", "m", w.now.Add(-2*time.Minute), 1000)
	w.call(acct2, "eu-west-1", "m", w.now.Add(-time.Minute), 1000)
	w.run()
	if w.bucket.Gets != 2 {
		t.Fatalf("want 2 files read across accounts and regions, got %d", w.bucket.Gets)
	}
	w.restart()
	w.now = w.now.Add(time.Minute)
	w.run()
	if w.bucket.Gets != 2 {
		t.Fatalf("a restart re-read files: %d gets", w.bucket.Gets)
	}
	w.call(acct1, "us-east-1", "m", w.now.Add(-time.Minute), 1000)
	w.now = w.now.Add(time.Minute)
	w.run()
	tot, _ := w.store.Totals(context.Background(), "ai", acct1, w.now)
	if w.bucket.Gets != 3 || tot.Hour != 2 {
		t.Fatalf("gets %d, hour total %v", w.bucket.Gets, tot.Hour)
	}
}

func TestLateFileCountsAtEventTime(t *testing.T) {
	w := newWorld(t)
	w.run() // discover nothing yet
	callAt := w.now.Add(-5 * time.Minute)
	w.now = w.now.Add(time.Minute)
	w.call(acct1, "us-east-1", "m", callAt, 3000)
	w.now = w.now.Add(10 * time.Minute) // past the next rediscovery
	w.run()
	tot, _ := w.store.Totals(context.Background(), "ai", acct1, callAt.Add(59*time.Minute))
	if tot.Hour != 3 {
		t.Fatalf("hour total at event time = %v", tot.Hour)
	}
	tot, _ = w.store.Totals(context.Background(), "ai", acct1, callAt.Add(61*time.Minute))
	if tot.Hour != 0 || tot.Day != 3 {
		t.Fatalf("after the hour rolls: %+v", tot)
	}
}

func TestCapCutsOffOnceAndLiftDetaches(t *testing.T) {
	w := newWorld(t)
	w.caps[acct1] = activity.Caps{Hourly: 5, Enforce: true}
	w.call(acct1, "us-east-1", "m", w.now.Add(-time.Minute), 6000)
	w.run()
	if !w.policies.Attached["p-ai/"+acct1] || w.count("cutoff ") != 1 || len(w.alerts) == 0 {
		t.Fatalf("not cut off: %v %v", w.policies.Calls, w.events)
	}
	w.call(acct1, "us-east-1", "m", w.now, 6000)
	w.now = w.now.Add(time.Minute)
	w.run()
	if w.count("cutoff ") != 1 || len(w.policies.Calls) != 1 {
		t.Fatalf("cut off twice: %v %v", w.policies.Calls, w.events)
	}
	cuts, _ := w.store.Cutoffs(context.Background(), "ai")
	w.lifts[cuts[0].ID] = true
	w.now = w.now.Add(time.Minute)
	w.run()
	if w.policies.Attached["p-ai/"+acct1] || w.count("cutoff_lifted ") != 1 {
		t.Fatalf("not lifted: %v %v", w.policies.Calls, w.events)
	}
	// Lifted while still over the cap: cut off again only on a new breach.
	w.now = w.now.Add(time.Minute)
	w.run()
	if w.policies.Attached["p-ai/"+acct1] {
		t.Fatal("cut off again with no new usage")
	}
	w.call(acct1, "us-east-1", "m", w.now, 1000)
	w.now = w.now.Add(time.Minute)
	w.run()
	if !w.policies.Attached["p-ai/"+acct1] || w.count("cutoff ") != 2 {
		t.Fatalf("a new breach after a lift must cut off again: %v", w.events)
	}
}

func TestRaisedCapLifts(t *testing.T) {
	w := newWorld(t)
	w.caps[acct1] = activity.Caps{Monthly: 5, Enforce: true}
	w.call(acct1, "us-east-1", "m", w.now.Add(-time.Minute), 6000)
	w.run()
	w.caps[acct1] = activity.Caps{Monthly: 50, Enforce: true}
	w.now = w.now.Add(time.Minute)
	w.run()
	if w.policies.Attached["p-ai/"+acct1] || w.count("cutoff_lifted ") != 1 {
		t.Fatalf("raised cap must lift: %v", w.events)
	}
}

func TestUnknownLiftDoesNothing(t *testing.T) {
	w := newWorld(t)
	w.lifts["ai:111122223333:20261111T0000"] = true
	w.run()
	if len(w.policies.Calls) != 0 || w.count("cutoff_lifted ") != 0 {
		t.Fatal("lifting an id that isn't cut off must do nothing")
	}
}

func TestDryRunAttachesNothingAndReportsHourly(t *testing.T) {
	w := newWorld(t)
	w.caps[acct1] = activity.Caps{Hourly: 1}
	w.call(acct1, "us-east-1", "m", w.now.Add(-time.Minute), 2000)
	w.run()
	if len(w.policies.Calls) != 0 || w.count("would_cutoff ") != 1 {
		t.Fatalf("dry-run: %v %v", w.policies.Calls, w.events)
	}
	for i := 0; i < 5; i++ {
		w.call(acct1, "us-east-1", "m", w.now, 2000)
		w.now = w.now.Add(time.Minute)
		w.run()
	}
	if w.count("would_cutoff ") != 1 {
		t.Fatalf("dry-run reported more than once within the hour: %d", w.count("would_cutoff "))
	}
}

func TestTripwireCutsOffAnAccountNoScopeCovers(t *testing.T) {
	w := newWorld(t)
	w.call(acct2, "us-east-1", "forbidden", w.now.Add(-time.Minute), 1)
	w.e.Reader.Base = base
	runs := []activity.PackRun{{Name: "ai", Pack: pricePack{}, PolicyID: "p-ai", DefaultEnforce: true, Lifts: map[string]bool{},
		CapsFor: func(string) (activity.Caps, bool) { return activity.Caps{}, false }}}
	if err := w.e.Tick(context.Background(), runs); err != nil {
		t.Fatal(err)
	}
	if !w.policies.Attached["p-ai/"+acct2] {
		t.Fatalf("tripwire did not cut off: %v", w.events)
	}
}

func TestMalformedFileIsSkippedAndAlerted(t *testing.T) {
	w := newWorld(t)
	k := activitytest.TrailKey(base, acct1, "us-east-1", w.now.Add(-time.Minute), "bad")
	w.bucket.Put(k, []byte("not gzip"), w.now)
	w.run()
	w.now = w.now.Add(time.Minute)
	w.run()
	n := 0
	for _, a := range w.alerts {
		if strings.Contains(a, "unreadable trail file") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want one alert for the bad file, got %d (%v)", n, w.alerts)
	}
}

func TestStallAndAccessError(t *testing.T) {
	w := newWorld(t)
	w.call(acct1, "us-east-1", "m", w.now.Add(-time.Minute), 1)
	w.run()
	w.now = w.now.Add(31 * time.Minute)
	w.run()
	if w.count("trail_stalled ") != 1 {
		t.Fatalf("no stall after 31 minutes: %v", w.events)
	}
	w.call(acct1, "us-east-1", "m", w.now, 1)
	w.now = w.now.Add(time.Minute)
	w.run()
	if w.count("trail_resumed ") != 1 {
		t.Fatalf("no resume: %v", w.events)
	}
	w.bucket.Deny = true
	w.now = w.now.Add(time.Minute)
	_ = w.e.Tick(context.Background(), nil)
	if w.count("trail_stalled ") != 2 || !strings.Contains(strings.Join(w.alerts, "|"), "trail stalled") {
		t.Fatalf("an access error must report a stall at once: %v", w.events)
	}
}

func TestHourlySummaries(t *testing.T) {
	w := newWorld(t)
	w.call(acct1, "us-east-1", "m1", w.now.Add(-time.Minute), 1000)
	w.call(acct1, "us-east-1", "m2", w.now.Add(-time.Minute), 1000)
	w.run()
	w.now = time.Date(2026, 11, 11, 10, 11, 0, 0, time.UTC) // 10 minutes after the hour ended
	w.run()
	if n := w.count("usage_summary "); n != 2 {
		t.Fatalf("want one summary per model for 09:00, got %d (%v)", n, w.events)
	}
	w.now = w.now.Add(time.Minute)
	w.run()
	if n := w.count("usage_summary "); n != 2 {
		t.Fatal("summaries written twice for the same hour")
	}
}

func TestSummaryCarriesAggregatesOnly(t *testing.T) {
	w := newWorld(t)
	var detail map[string]any
	w.e.Emit = func(_ context.Context, kind, _, _ string, d map[string]any) {
		if kind == activity.KindUsage {
			detail = d
		}
	}
	w.call(acct1, "us-east-1", "m1", w.now.Add(-time.Minute), 1000)
	w.run()
	w.now = time.Date(2026, 11, 11, 10, 11, 0, 0, time.UTC)
	w.run()
	want := map[string]bool{"pack": true, "account": true, "model": true, "hour": true, "calls": true, "input_tokens": true, "output_tokens": true, "estimated_usd": true}
	if len(detail) != len(want) {
		t.Fatalf("summary detail %v", detail)
	}
	for k := range detail {
		if !want[k] {
			t.Fatalf("summary carries %q; only aggregates may leave AWS", k)
		}
	}
}

func TestFailedUsageWriteRereadsAndAlertsOnce(t *testing.T) {
	w := newWorld(t)
	w.caps[acct1] = activity.Caps{Hourly: 1, Enforce: true}
	w.call(acct1, "us-east-1", "m", w.now.Add(-time.Minute), 2000) // $2
	w.store.FailUsage = true
	for i := 0; i < 2; i++ {
		if err := w.e.Tick(context.Background(), w.runs()); err == nil {
			t.Fatal("a failed usage write must fail the tick")
		}
		w.restart()
		w.now = w.now.Add(time.Minute)
	}
	if w.bucket.Gets != 2 {
		t.Fatalf("the file must be read again while writes fail: %d gets", w.bucket.Gets)
	}
	if got := strings.Join(w.alerts, "|"); got != "guardrail: AI spend not counted" {
		t.Fatalf("want one not-counted alert, got %q", got)
	}
	w.store.FailUsage = false
	w.run()
	tot, _ := w.store.Totals(context.Background(), "ai", acct1, w.now)
	if tot.Hour != 2 || !w.policies.Attached["p-ai/"+acct1] {
		t.Fatalf("once writes work the call must count and cut off: total %v, events %v", tot.Hour, w.events)
	}
	if !strings.Contains(strings.Join(w.alerts, "|"), "guardrail: AI spend counted again") {
		t.Fatalf("no recovery alert: %v", w.alerts)
	}
	w.now = w.now.Add(time.Minute)
	w.run()
	if w.bucket.Gets != 3 {
		t.Fatalf("once committed the file must not be read again: %d gets", w.bucket.Gets)
	}
}
