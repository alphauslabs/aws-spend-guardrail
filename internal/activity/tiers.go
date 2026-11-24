package activity

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// StreamTierWindow is how long the engine remembers a ConverseStream call
// record's tier for its completion record (add-tier-pricing D7).
const StreamTierWindow = 15 * time.Minute

// maxStreamTiers caps the remembered calls, so the state item stays well
// under DynamoDB's 400 KB; the oldest go first.
const maxStreamTiers = 5000

// metaStreamTiers names the remembered ConverseStream calls.
const metaStreamTiers = "stream_tiers"

// streamTier is a ConverseStream call record's tier ("" for the standard
// tier) and when it was seen.
type streamTier struct {
	Tier string `json:"t,omitempty"`
	At   int64  `json:"a"` // Unix seconds
}

// streamTiers are the ConverseStream calls remembered by request id.
type streamTiers struct {
	store   Store
	calls   map[string]streamTier
	changed bool
}

// joinStreamTiers gives each ConverseStream completion record, which carries
// the token counts but no tier, the tier of the call record it completes, by
// its parentRequestId (add-tier-pricing D7). A completion whose call record
// the guard hasn't seen, in this tick or in the last StreamTierWindow, is
// marked TierUnknown and priced at its model's highest tier. Call records are
// remembered for the next ticks; save stores them once the tick's usage is.
func (e *Engine) joinStreamTiers(ctx context.Context, now time.Time, records []Record) (*streamTiers, error) {
	st := &streamTiers{store: e.Store, calls: map[string]streamTier{}}
	if _, err := e.Store.GetMeta(ctx, metaStreamTiers, &st.calls); err != nil {
		// Without the memory, every completion matches only this tick.
		st.calls = map[string]streamTier{}
		for i := range records {
			st.mark(records, i, now)
		}
		st.resolve(records)
		return st, fmt.Errorf("read %s: %w", metaStreamTiers, err)
	}
	if st.calls == nil {
		st.calls = map[string]streamTier{}
	}
	cutoff := now.Add(-StreamTierWindow).Unix()
	for id, c := range st.calls {
		if c.At < cutoff {
			delete(st.calls, id)
			st.changed = true
		}
	}
	for i := range records {
		st.mark(records, i, now)
	}
	st.resolve(records)
	st.trim()
	return st, nil
}

// mark remembers a ConverseStream call record's tier.
func (st *streamTiers) mark(records []Record, i int, now time.Time) {
	r := records[i]
	if r.EventName != "ConverseStream" || r.EventType != "AwsApiCall" || r.RequestID == "" || r.ErrorCode != "" {
		return
	}
	st.calls[r.RequestID] = streamTier{Tier: r.ServiceTier, At: now.Unix()}
	st.changed = true
}

// resolve gives ConverseStream completions their call records' tiers.
func (st *streamTiers) resolve(records []Record) {
	for i := range records {
		r := &records[i]
		if r.EventName != "ConverseStream" || r.EventType != "AwsServiceEvent" || r.ServiceTier != "" {
			continue
		}
		if c, ok := st.calls[r.ParentRequestID]; ok && r.ParentRequestID != "" {
			r.ServiceTier = c.Tier
			continue
		}
		r.TierUnknown = true
	}
}

// trim drops the oldest calls beyond maxStreamTiers.
func (st *streamTiers) trim() {
	if len(st.calls) <= maxStreamTiers {
		return
	}
	ids := make([]string, 0, len(st.calls))
	for id := range st.calls {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return st.calls[ids[i]].At < st.calls[ids[j]].At })
	for _, id := range ids[:len(ids)-maxStreamTiers] {
		delete(st.calls, id)
	}
	st.changed = true
}

// save stores the remembered calls when they changed.
func (st *streamTiers) save(ctx context.Context) error {
	if st == nil || !st.changed {
		return nil
	}
	return st.store.PutMeta(ctx, metaStreamTiers, st.calls)
}

// UltrafastTokens is one account and model's ultrafast input and output
// tokens over a day, from the shared metrics (add-tier-pricing D10).
type UltrafastTokens struct {
	Account, Model string
	In, Out        int64
}

// metaUltrafastSeen names the accounts and models the trail showed
// ultrafast calls for, per UTC day (add-tier-pricing D10).
const metaUltrafastSeen = "ultrafast_seen"

// UltrafastSeen is, per UTC day ("2006-01-02"), the "account|model id" pairs
// the trail showed ultrafast calls for. The engine keeps today and yesterday.
type UltrafastSeen map[string]map[string]bool

// noteUltrafast records the tick's ultrafast calls, and drops days before
// yesterday. It saves only when something changed.
func (e *Engine) noteUltrafast(ctx context.Context, now time.Time, records []Record) error {
	var fresh []Record
	for _, r := range records {
		if r.ServiceTier == "ultrafast" && r.ErrorCode == "" && r.ModelID != "" {
			fresh = append(fresh, r)
		}
	}
	seen := UltrafastSeen{}
	if _, err := e.Store.GetMeta(ctx, metaUltrafastSeen, &seen); err != nil {
		return fmt.Errorf("read %s: %w", metaUltrafastSeen, err)
	}
	if seen == nil {
		seen = UltrafastSeen{}
	}
	changed := false
	keep := map[string]bool{now.UTC().Format("2006-01-02"): true, now.UTC().AddDate(0, 0, -1).Format("2006-01-02"): true}
	for day := range seen {
		if !keep[day] {
			delete(seen, day)
			changed = true
		}
	}
	for _, r := range fresh {
		day := r.EventTime.UTC().Format("2006-01-02")
		if !keep[day] {
			continue
		}
		k := r.Account + "|" + r.ModelID
		if seen[day] == nil {
			seen[day] = map[string]bool{}
		}
		if !seen[day][k] {
			seen[day][k] = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return e.Store.PutMeta(ctx, metaUltrafastSeen, seen)
}

// UltrafastSeenOn returns the "account|model id" pairs the trail showed
// ultrafast calls for on a UTC day.
func UltrafastSeenOn(ctx context.Context, store Store, day time.Time) (map[string]bool, error) {
	seen := UltrafastSeen{}
	if _, err := store.GetMeta(ctx, metaUltrafastSeen, &seen); err != nil {
		return nil, fmt.Errorf("read %s: %w", metaUltrafastSeen, err)
	}
	return seen[day.UTC().Format("2006-01-02")], nil
}

// reportUnpricedTiers reports, once per pack, model, and tier, a call on a
// tier the tables don't price for its model: an unpriced_tier event and an
// alert (add-tier-pricing D5).
func (e *Engine) reportUnpricedTiers(ctx context.Context, run PackRun, counted map[string]Usage) error {
	return e.reportOnce(ctx, "unpriced_tier."+run.Name, counted, func(key string, u Usage) {
		e.Emit(ctx, KindUnpricedTier, fmt.Sprintf("%s:%s:%s:%s", KindUnpricedTier, run.Name, u.Model, u.UnpricedTier), "account:"+u.Account, map[string]any{
			"pack": run.Name, "model": u.Model, "tier": u.UnpricedTier, "account": u.Account, "caller": u.Caller, "region": u.Region,
		})
		e.Alert(ctx, fmt.Sprintf("guardrail: %s tier of %s has no price", u.UnpricedTier, u.Model), fmt.Sprintf(
			"Account %s called %s on the %s service tier, which the guard's prices don't cover for that model. The %s pack counts these calls at the model's highest known tier price, so their spend may be overestimated. A release or a shared price publish that lists the tier corrects it.",
			u.Account, u.Model, u.UnpricedTier, run.Name))
	})
}

// reportUnmeasured reports, once per pack, account, and model, a call the
// guard counted at $0 because its records carried no token counts, under
// ai.unmeasured_calls allow: an unmeasured_call event and an alert
// (add-tier-pricing D9).
func (e *Engine) reportUnmeasured(ctx context.Context, run PackRun, counted map[string]Usage) error {
	return e.reportOnce(ctx, "unmeasured."+run.Name, counted, func(key string, u Usage) {
		e.Emit(ctx, KindUnmeasured, fmt.Sprintf("%s:%s:%s:%s", KindUnmeasured, run.Name, u.Account, u.Model), "account:"+u.Account, map[string]any{
			"pack": run.Name, "model": u.Model, "account": u.Account, "caller": u.Caller, "region": u.Region, "unmeasured_calls": "allow",
		})
		e.Alert(ctx, fmt.Sprintf("guardrail: calls to %s can't be measured", u.Model), fmt.Sprintf(
			"Account %s called %s in %s in a way CloudTrail records without token counts, such as a streamed Chat Completions call. The config's ai.unmeasured_calls is allow, so the %s pack counts these calls at $0: their spend isn't counted toward the account's caps.",
			u.Account, u.Model, u.Region, run.Name))
	})
}

// reportOnce calls report for each key not reported before, in key order,
// and remembers the keys under name in the state table.
func (e *Engine) reportOnce(ctx context.Context, name string, counted map[string]Usage, report func(key string, u Usage)) error {
	if len(counted) == 0 {
		return nil
	}
	seen := map[string]unpricedSeen{}
	if _, err := e.Store.GetMeta(ctx, name, &seen); err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	keys := make([]string, 0, len(counted))
	for k := range counted {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	changed := false
	for _, k := range keys {
		if _, ok := seen[k]; ok {
			continue
		}
		u := counted[k]
		seen[k] = unpricedSeen{Account: u.Account, At: u.Time}
		changed = true
		report(k, u)
	}
	if !changed {
		return nil
	}
	return e.Store.PutMeta(ctx, name, seen)
}
