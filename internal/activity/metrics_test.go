package activity_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
)

// fakeMetrics is one region's mantle usage as CloudWatch reports it.
type fakeMetrics struct {
	points []activity.MetricPoint
	err    error
	reads  int
}

func (f *fakeMetrics) Usage(_ context.Context, from, to time.Time) ([]activity.MetricPoint, error) {
	f.reads++
	if f.err != nil {
		return nil, f.err
	}
	var out []activity.MetricPoint
	for _, p := range f.points {
		if !p.Minute.Before(from) && p.Minute.Before(to) {
			out = append(out, p)
		}
	}
	return out, nil
}

// mantlePack prices mantle usage at $1 per 1,000 input tokens, counting the
// record's calls.
type mantlePack struct{}

func (mantlePack) Wants(r activity.Record) bool { return r.EventSource == activity.MantleSource }
func (mantlePack) Evaluate(r activity.Record) (*activity.Usage, *activity.Hit) {
	return &activity.Usage{Account: r.Account, Time: r.EventTime, Model: r.ModelID, Region: r.Region, Calls: r.Calls, In: r.InputTokens, Out: r.OutputTokens, USD: float64(r.InputTokens) / 1000}, nil
}

type mantleWorld struct {
	*world
	src *fakeMetrics
}

func newMantleWorld(t *testing.T) *mantleWorld {
	w := &mantleWorld{world: newWorld(t), src: &fakeMetrics{}}
	w.now = w.now.Add(30 * time.Minute) // mid-hour, so the minutes read stay in one hour
	w.attach()
	return w
}

// attach gives the (re)started engine its metrics reader.
func (w *mantleWorld) attach() {
	w.e.Metrics = []*activity.MetricsReader{{Region: "us-east-1", Source: w.src, Store: w.store}}
}

func (w *mantleWorld) tick() error {
	return w.e.Tick(context.Background(), []activity.PackRun{{Name: "ai", Pack: mantlePack{}, PolicyID: "p-ai", Lifts: map[string]bool{},
		CapsFor: func(a string) (activity.Caps, bool) { c, ok := w.caps[a]; return c, ok }}})
}

func (w *mantleWorld) hour(t *testing.T) (calls, in int64) {
	t.Helper()
	sums, err := w.store.HourSummaries(context.Background(), "ai", w.now.Truncate(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sums {
		calls, in = calls+s.Calls, in+s.In
	}
	return calls, in
}

// A minute's datapoint that grows is counted for its growth only, and one
// that shrinks is ignored (add-mantle-usage D3).
func TestMantleMinuteCountsOnce(t *testing.T) {
	w := newMantleWorld(t)
	minute := w.now.Add(-2 * time.Minute).Truncate(time.Minute)
	w.src.points = []activity.MetricPoint{{Account: acct1, Model: "openai.gpt-oss-20b", Minute: minute, Calls: 1, In: 12, Out: 14}}
	if err := w.tick(); err != nil {
		t.Fatal(err)
	}
	if calls, in := w.hour(t); calls != 1 || in != 12 {
		t.Fatalf("first read: %d calls, %d in", calls, in)
	}
	w.now = w.now.Add(time.Minute)
	w.src.points[0].Calls, w.src.points[0].In = 2, 30
	if err := w.tick(); err != nil {
		t.Fatal(err)
	}
	if calls, in := w.hour(t); calls != 2 || in != 30 {
		t.Fatalf("grown: %d calls, %d in; want 2, 30", calls, in)
	}
	w.now = w.now.Add(time.Minute)
	w.src.points[0].In = 20 // a partial read: ignored
	w.restart()
	w.attach()
	if err := w.tick(); err != nil {
		t.Fatal(err)
	}
	if calls, in := w.hour(t); calls != 2 || in != 30 {
		t.Fatalf("after a shrink and a restart: %d calls, %d in; want 2, 30", calls, in)
	}
}

// A failed usage write rereads the same minutes, and counts them once
// storing works (add-mantle-usage D3).
func TestMantleFailedWriteCountsOnce(t *testing.T) {
	w := newMantleWorld(t)
	w.src.points = []activity.MetricPoint{{Account: acct1, Model: "m", Minute: w.now.Add(-time.Minute).Truncate(time.Minute), Calls: 3, In: 300, Out: 30}}
	w.store.FailUsage = true
	for i := 0; i < 2; i++ {
		if err := w.tick(); err == nil {
			t.Fatal("a failed usage write must fail the tick")
		}
		w.restart()
		w.attach()
		w.now = w.now.Add(time.Minute)
	}
	w.store.FailUsage = false
	if err := w.tick(); err != nil {
		t.Fatal(err)
	}
	if calls, in := w.hour(t); calls != 3 || in != 300 {
		t.Fatalf("after recovery: %d calls, %d in; want 3, 300", calls, in)
	}
	w.now = w.now.Add(time.Minute)
	if err := w.tick(); err != nil {
		t.Fatal(err)
	}
	if calls, _ := w.hour(t); calls != 3 {
		t.Fatalf("counted twice: %d calls", calls)
	}
}

// A region whose metrics can't be read alerts once and again when it reads,
// without failing the tick; usage still inside the window is counted on
// recovery (add-mantle-usage D5).
func TestMantleRegionErrorAlertsOnce(t *testing.T) {
	w := newMantleWorld(t)
	w.src.err = errors.New("AccessDenied: not linked")
	w.src.points = []activity.MetricPoint{{Account: acct1, Model: "m", Minute: w.now.Add(-time.Minute).Truncate(time.Minute), Calls: 1, In: 100}}
	for i := 0; i < 3; i++ {
		if err := w.tick(); err != nil {
			t.Fatalf("a metrics failure must not fail the tick: %v", err)
		}
		w.now = w.now.Add(time.Minute)
	}
	if got := strings.Join(w.alerts, "|"); got != "guardrail: mantle spend in us-east-1 not watched" {
		t.Fatalf("alerts %q", got)
	}
	w.src.err = nil
	if err := w.tick(); err != nil {
		t.Fatal(err)
	}
	if n := len(w.alerts); n != 2 || w.alerts[1] != "guardrail: mantle spend in us-east-1 watched again" {
		t.Fatalf("alerts %v", w.alerts)
	}
	if calls, in := w.hour(t); calls != 1 || in != 100 {
		t.Fatalf("usage missed while failing: %d calls, %d in", calls, in)
	}
}

// Minutes older than the window are dropped from state and not read again.
func TestMantleWindow(t *testing.T) {
	w := newMantleWorld(t)
	old := w.now.Add(-20 * time.Minute).Truncate(time.Minute)
	w.src.points = []activity.MetricPoint{{Account: acct1, Model: "m", Minute: old, Calls: 1, In: 100}}
	if err := w.tick(); err != nil {
		t.Fatal(err)
	}
	if calls, _ := w.hour(t); calls != 0 {
		t.Fatalf("a minute older than the window was counted: %d", calls)
	}
}
