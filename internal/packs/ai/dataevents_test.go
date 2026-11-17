package ai

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity/activitytest"
)

// The fixture's records, captured on a trail logging Bedrock model data
// events and scrubbed (fix-data-event-double-count): call aa logged as both a
// management and a data event, call bb as a management event only, and a
// data-only bidirectional stream.
const fixture = "../../activity/testdata/bedrock-data-events.json"

func fixtureRecords(t *testing.T, keep func(map[string]any) bool) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Records []map[string]any }
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, r := range f.Records {
		if keep == nil || keep(r) {
			out = append(out, r)
		}
	}
	return out
}

type dataWorld struct {
	bucket   *activitytest.Bucket
	store    *activitytest.Store
	policies *activitytest.Policies
	events   []string
	e        *activity.Engine
	now      time.Time
}

const fixtureAcct, fixtureBase = "111122223333", "AWSLogs/o-test/"

// newDataWorld puts the records in one trail file of the fixture's account
// and runs one tick of the engine with the AI pack.
func newDataWorld(t *testing.T, cfg *config.ResolvedAI, records []map[string]any) *dataWorld {
	t.Helper()
	w := &dataWorld{bucket: activitytest.NewBucket(), store: activitytest.NewStore(), policies: activitytest.NewPolicies(),
		now: time.Date(2026, 11, 17, 2, 58, 0, 0, time.UTC)}
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if err := json.NewEncoder(zw).Encode(map[string]any{"Records": records}); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	k := activitytest.TrailKey(fixtureBase, fixtureAcct, "us-east-1", w.now.Add(-3*time.Minute), "a")
	w.bucket.Put(k, b.Bytes(), w.now)
	w.e = &activity.Engine{
		Reader: &activity.Reader{Bucket: w.bucket, Store: w.store, Base: fixtureBase}, Store: w.store, Policies: w.policies,
		Emit: func(_ context.Context, kind, id, _ string, _ map[string]any) {
			w.events = append(w.events, kind+" "+id)
		},
		Alert: func(context.Context, string, string) {},
		Now:   func() time.Time { return w.now },
	}
	runs := []activity.PackRun{{Name: "ai", Pack: New(table(t), cfg), PolicyID: "p-ai", DefaultEnforce: true, Lifts: map[string]bool{},
		CapsFor: func(string) (activity.Caps, bool) { return activity.Caps{Monthly: 1000, Enforce: true}, true }}}
	if err := w.e.Tick(context.Background(), runs); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *dataWorld) hour(t *testing.T) (calls, in, out int64) {
	t.Helper()
	sums, err := w.store.HourSummaries(context.Background(), "ai", time.Date(2026, 11, 17, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sums {
		calls, in, out = calls+s.Calls, in+s.In, out+s.Out
	}
	return calls, in, out
}

func TestCallLoggedTwiceCountedOnce(t *testing.T) {
	w := newDataWorld(t, nil, fixtureRecords(t, nil))
	// aa (3 in, 20 out) once, bb (5 in, 7 out); the data-only stream is not read.
	if calls, in, out := w.hour(t); calls != 2 || in != 8 || out != 27 {
		t.Fatalf("hour: %d calls, %d in, %d out; want 2, 8, 27", calls, in, out)
	}
}

func TestTripwireOnCallLoggedTwiceFiresOnce(t *testing.T) {
	onlyAA := func(r map[string]any) bool { return r["requestID"] == "00000000-0000-4000-8000-0000000000aa" }
	recs := fixtureRecords(t, onlyAA)
	if len(recs) != 2 {
		t.Fatalf("fixture: %d records for call aa, want 2", len(recs))
	}
	p := New(table(t), &config.ResolvedAI{Models: []string{"anthropic.claude-haiku-5-5"}})
	hits := 0
	for _, raw := range recs {
		r := parseOne(t, raw)
		if !p.Wants(r) {
			continue
		}
		if _, h := p.Evaluate(r); h != nil {
			hits++
		}
	}
	if hits != 1 {
		t.Fatalf("the pack hit %d times on one call, want 1", hits)
	}
	w := newDataWorld(t, &config.ResolvedAI{Models: []string{"anthropic.claude-haiku-5-5"}}, recs)
	n := 0
	for _, e := range w.events {
		if strings.HasPrefix(e, "cutoff ") {
			n++
		}
	}
	if n != 1 || !w.policies.Attached["p-ai/"+fixtureAcct] {
		t.Fatalf("events %v, attached %v", w.events, w.policies.Attached)
	}
}

func TestManagementOnlyTrailUnchanged(t *testing.T) {
	mgmt := func(r map[string]any) bool { return r["eventCategory"] == "Management" }
	w := newDataWorld(t, nil, fixtureRecords(t, mgmt))
	if calls, in, out := w.hour(t); calls != 2 || in != 8 || out != 27 {
		t.Fatalf("hour: %d calls, %d in, %d out; want 2, 8, 27", calls, in, out)
	}
}

// parseOne runs one raw record through the trail parser.
func parseOne(t *testing.T, raw map[string]any) activity.Record {
	t.Helper()
	recs, err := activity.ParseFile(activitytest.TrailFile(raw))
	if err != nil || len(recs) != 1 {
		t.Fatalf("parse: %v, %d records", err, len(recs))
	}
	return recs[0]
}
