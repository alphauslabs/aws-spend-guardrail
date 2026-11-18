package activity

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// MetricPoint is one account and model's mantle usage in one minute, as
// CloudWatch reports it (add-mantle-usage D2).
type MetricPoint struct {
	Account, Model string
	Minute         time.Time
	Calls, In, Out int64
}

// MetricsSource reads mantle usage for one region.
type MetricsSource interface {
	Usage(ctx context.Context, from, to time.Time) ([]MetricPoint, error)
}

// MantleWindow is how far back the metrics reader revisits minutes
// (add-mantle-usage D3).
const MantleWindow = 15 * time.Minute

// MantleSource is the event source of mantle usage records.
const MantleSource = "bedrock-mantle.amazonaws.com"

// MetricsReader turns one region's mantle usage metrics into records, each
// minute's growth once (add-mantle-usage D1, D3).
type MetricsReader struct {
	Region  string
	Source  MetricsSource
	Store   Store
	pending *metricsState
}

// metricsState is what a region's reader has counted, per account, model,
// and minute, for the minutes still inside the window.
type metricsState struct {
	Counted map[string]counted `json:"counted"`
}

type counted struct {
	Calls int64 `json:"calls"`
	In    int64 `json:"in"`
	Out   int64 `json:"out"`
}

func (m *MetricsReader) meta() string { return "mantle." + m.Region }

// ReadNew reads the last MantleWindow of usage and returns a record for each
// account, model, and minute that grew since it was last counted. A value
// that shrank is ignored. The counted values are saved only by Commit.
func (m *MetricsReader) ReadNew(ctx context.Context, now time.Time) ([]Record, error) {
	st := metricsState{Counted: map[string]counted{}}
	if _, err := m.Store.GetMeta(ctx, m.meta(), &st); err != nil {
		return nil, fmt.Errorf("read mantle state for %s: %w", m.Region, err)
	}
	if st.Counted == nil {
		st.Counted = map[string]counted{}
	}
	from := now.Truncate(time.Minute).Add(-MantleWindow)
	points, err := m.Source.Usage(ctx, from, now)
	if err != nil {
		return nil, err
	}
	sort.Slice(points, func(i, j int) bool { return pointKey(points[i]) < pointKey(points[j]) })
	var out []Record
	for _, p := range points {
		if p.Minute.Before(from) {
			continue
		}
		k := pointKey(p)
		prev := st.Counted[k]
		grow := counted{Calls: growth(p.Calls, prev.Calls), In: growth(p.In, prev.In), Out: growth(p.Out, prev.Out)}
		st.Counted[k] = counted{Calls: max(p.Calls, prev.Calls), In: max(p.In, prev.In), Out: max(p.Out, prev.Out)}
		if grow == (counted{}) {
			continue
		}
		out = append(out, Record{
			EventTime: p.Minute.UTC(), EventSource: MantleSource, EventName: "CreateInference",
			EventType: "MetricDatapoint", EventCategory: CategoryMetric, Region: m.Region, Account: p.Account,
			ModelID: p.Model, InputTokens: grow.In, OutputTokens: grow.Out, Calls: grow.Calls,
		})
	}
	for k := range st.Counted {
		if t, ok := keyMinute(k); !ok || t.Before(from) {
			delete(st.Counted, k)
		}
	}
	m.pending = &st
	return out, nil
}

// Commit saves what the last ReadNew counted. The engine calls it only once
// the records' usage is stored.
func (m *MetricsReader) Commit(ctx context.Context) error {
	if m.pending == nil {
		return nil
	}
	if err := m.Store.PutMeta(ctx, m.meta(), m.pending); err != nil {
		return err
	}
	m.pending = nil
	return nil
}

func growth(now, before int64) int64 {
	if now > before {
		return now - before
	}
	return 0
}

// pointKey is "<minute>|<account>|<model>", so keys sort by minute.
func pointKey(p MetricPoint) string {
	return p.Minute.UTC().Format(time.RFC3339) + "|" + p.Account + "|" + p.Model
}

func keyMinute(k string) (time.Time, bool) {
	i := strings.Index(k, "|")
	if i < 0 {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, k[:i])
	return t, err == nil
}

// metaMantleStatus names the per-region read status of mantle metrics.
const metaMantleStatus = "mantle.status"

// mantleStatus maps each region whose metrics can't be read to why.
type mantleStatus struct {
	Failing map[string]string `json:"failing,omitempty"`
}

// readMetrics reads every region's mantle usage into records, and returns
// the readers that read, to commit once usage is stored. A region that fails
// is skipped for the tick and alerted once, and again when it reads again
// (add-mantle-usage D5); a failure doesn't fail the tick.
func (e *Engine) readMetrics(ctx context.Context, now time.Time, records *[]Record) []*MetricsReader {
	if len(e.Metrics) == 0 {
		return nil
	}
	var st mantleStatus
	_, _ = e.Store.GetMeta(ctx, metaMantleStatus, &st)
	if st.Failing == nil {
		st.Failing = map[string]string{}
	}
	changed := false
	var read []*MetricsReader
	for _, m := range e.Metrics {
		recs, err := m.ReadNew(ctx, now)
		if err != nil {
			if _, failing := st.Failing[m.Region]; !failing {
				e.Alert(ctx, "guardrail: mantle spend in "+m.Region+" not watched",
					"The AI guard can't read bedrock-mantle usage in "+m.Region+" from CloudWatch, so mantle calls there are not counted toward caps. Check the observability sink in the payer account in "+m.Region+" and its links (see docs/ai-guard.md). Totals are kept, and usage of the last 15 minutes is counted once reading works.\nError: "+err.Error())
				changed = true
			}
			st.Failing[m.Region] = err.Error()
			continue
		}
		if _, failing := st.Failing[m.Region]; failing {
			delete(st.Failing, m.Region)
			changed = true
			e.Alert(ctx, "guardrail: mantle spend in "+m.Region+" watched again", "The AI guard reads bedrock-mantle usage in "+m.Region+" again.")
		}
		*records = append(*records, recs...)
		read = append(read, m)
	}
	if changed {
		_ = e.Store.PutMeta(ctx, metaMantleStatus, st)
	}
	return read
}
