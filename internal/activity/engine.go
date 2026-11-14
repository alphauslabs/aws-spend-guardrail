package activity

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/internal/alerttext"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// Usage is one priced call.
type Usage struct {
	Account string
	Time    time.Time // when CloudTrail recorded the call
	Model   string
	Caller  string
	Region  string
	Calls   int64
	In, Out int64
	USD     float64
}

// Hit is a tripwire firing: cut off at once, no estimate needed.
type Hit struct {
	Account string
	Time    time.Time
	Rule    string // e.g. "model_not_allowed"
	Detail  string
}

// Pack recognizes and prices records for one kind of spend.
type Pack interface {
	// Wants reports whether the pack handles a record.
	Wants(r Record) bool
	// Evaluate prices a record or fires a tripwire. Either may be nil.
	Evaluate(r Record) (*Usage, *Hit)
}

// Caps are an account's spend caps in USD; zero means no cap.
type Caps struct {
	Hourly, Daily, Monthly float64
	Enforce                bool // false: dry-run
}

// PackRun is one pack as configured for a tick.
type PackRun struct {
	Name     string // "ai"
	Pack     Pack
	PolicyID string // the pack's deny SCP
	// CapsFor returns the account's caps, and whether any scope covers it.
	CapsFor func(account string) (Caps, bool)
	// Enforce reports whether a tripwire cutoff attaches the policy for an
	// account no scope covers.
	DefaultEnforce bool
	Lifts          map[string]bool
	// Detail is added to every cutoff and would-cutoff event, e.g. the price
	// version in use.
	Detail map[string]any
}

// Totals are an account's estimated spend for one pack.
type Totals struct {
	Hour, Day, Month float64
}

// Cutoff is an account cut off (or that would be, in dry-run) by a pack.
type Cutoff struct {
	ID       string    `json:"id"`
	Pack     string    `json:"pack"`
	Account  string    `json:"account"`
	At       time.Time `json:"at"`
	Rule     string    `json:"rule"`              // hourly_cap, daily_cap, monthly_cap, or a tripwire
	CapUSD   float64   `json:"cap_usd,omitempty"` // the cap passed, for cap rules
	Detail   string    `json:"detail"`
	Attached bool      `json:"attached"` // false: dry-run
}

// Summary is one account and model's usage for an hour.
type Summary struct {
	Account string
	Model   string
	Calls   int64
	In, Out int64
	USD     float64
}

// Store keeps the engine's state.
type Store interface {
	// AddUsage adds a priced call to the minute, hour-by-model, day and month
	// counters of its account.
	AddUsage(ctx context.Context, pack string, u Usage) error
	Totals(ctx context.Context, pack, account string, now time.Time) (Totals, error)
	HourSummaries(ctx context.Context, pack string, hour time.Time) ([]Summary, error)
	Cutoffs(ctx context.Context, pack string) ([]Cutoff, error)
	PutCutoff(ctx context.Context, c Cutoff) error
	DeleteCutoff(ctx context.Context, pack, account string) error
	// GetMeta and PutMeta keep small engine state (reader position, stall
	// status, last summary hour) as JSON under a name.
	GetMeta(ctx context.Context, name string, v any) (bool, error)
	PutMeta(ctx context.Context, name string, v any) error
}

// Policies attaches and detaches a pack's deny policy on an account.
type Policies interface {
	Attach(ctx context.Context, policyID, account string) error
	Detach(ctx context.Context, policyID, account string) error
}

// Engine runs the fast path once per tick.
type Engine struct {
	Reader   *Reader
	Store    Store
	Policies Policies
	// Emit records an inbox event; Alert publishes a local alert. Neither
	// waits on Jennah.
	Emit  func(ctx context.Context, kind, id, scope string, detail map[string]any)
	Alert func(ctx context.Context, subject, msg string)
	Now   func() time.Time
}

// Event kinds the engine emits, defined with the inbox layout.
const (
	KindCutoff      = schema.KindCutoff
	KindWouldCutoff = schema.KindWouldCutoff
	KindLift        = schema.KindCutoffLift
	KindTrailStall  = schema.KindTrailStall
	KindTrailResume = schema.KindTrailResume
	KindUsage       = schema.KindUsage
)

// StallAfter is how long without a new trail file before alerting.
const StallAfter = 30 * time.Minute

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

// Tick reads new trail records, updates totals, cuts off or lifts, and
// writes hourly summaries. It never waits on Jennah.
func (e *Engine) Tick(ctx context.Context, runs []PackRun) error {
	now := e.now()
	var errs []error

	records, readErr := e.Reader.ReadNew(ctx, now)
	e.trackStall(ctx, now, readErr)
	for _, k := range e.Reader.Bad() {
		e.Alert(ctx, "guardrail: unreadable trail file", "The trail file "+k+" could not be parsed and was skipped; calls recorded in it are not counted.")
	}
	if readErr != nil {
		errs = append(errs, readErr)
	}

	var usageErr error
	for _, run := range runs {
		hits := map[string]Hit{}
		active := map[string]bool{}
		for _, r := range records {
			if !run.Pack.Wants(r) {
				continue
			}
			u, h := run.Pack.Evaluate(r)
			if u != nil {
				if err := e.Store.AddUsage(ctx, run.Name, *u); err != nil {
					errs = append(errs, err)
					usageErr = err
				}
				active[u.Account] = true
			}
			if h != nil {
				if _, seen := hits[h.Account]; !seen {
					hits[h.Account] = *h
				}
			}
		}
		if err := e.evaluate(ctx, run, now, active, hits); err != nil {
			errs = append(errs, err)
		}
		if err := e.lifts(ctx, run, now); err != nil {
			errs = append(errs, err)
		}
		if err := e.summaries(ctx, run, now); err != nil {
			errs = append(errs, err)
		}
	}
	// Move the reader on only once every priced call is stored; otherwise the
	// same files are read again next tick. After a partial failure some calls
	// are counted twice, which can only cut off sooner, never later.
	if usageErr == nil {
		if err := e.Reader.Commit(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	e.trackCounting(ctx, now, usageErr)
	return errors.Join(errs...)
}

func (e *Engine) evaluate(ctx context.Context, run PackRun, now time.Time, active map[string]bool, hits map[string]Hit) error {
	existing, err := e.Store.Cutoffs(ctx, run.Name)
	if err != nil {
		return fmt.Errorf("read cutoffs: %w", err)
	}
	cut := map[string]Cutoff{}
	for _, c := range existing {
		cut[c.Account] = c
	}
	var errs []error
	accounts := map[string]bool{}
	for a := range active {
		accounts[a] = true
	}
	for a := range hits {
		accounts[a] = true
	}
	for _, acct := range sortedKeys(accounts) {
		if c, ok := cut[acct]; ok && c.Attached {
			continue // already cut off
		}
		caps, covered := run.CapsFor(acct)
		enforce := run.DefaultEnforce
		if covered {
			enforce = caps.Enforce
		}
		var c *Cutoff
		if h, ok := hits[acct]; ok {
			c = &Cutoff{Rule: h.Rule, Detail: h.Detail}
		} else if covered {
			t, err := e.Store.Totals(ctx, run.Name, acct, now)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, chk := range []struct {
				rule       string
				cap, total float64
			}{{"hourly_cap", caps.Hourly, t.Hour}, {"daily_cap", caps.Daily, t.Day}, {"monthly_cap", caps.Monthly, t.Month}} {
				if chk.cap > 0 && chk.total > chk.cap {
					c = &Cutoff{Rule: chk.rule, CapUSD: chk.cap, Detail: fmt.Sprintf("estimated %s %.2f USD, cap %.2f USD", strings.TrimSuffix(chk.rule, "_cap"), chk.total, chk.cap)}
					break
				}
			}
		}
		if c == nil {
			continue
		}
		if prev, ok := cut[acct]; ok && !prev.Attached && !enforce && prev.Rule == c.Rule && now.Sub(prev.At) < time.Hour {
			continue // dry-run already reported this within the hour
		}
		c.ID = fmt.Sprintf("%s:%s:%s", run.Name, acct, now.Format("20060102T1504"))
		c.Pack, c.Account, c.At, c.Attached = run.Name, acct, now, enforce
		if enforce {
			if err := e.Policies.Attach(ctx, run.PolicyID, acct); err != nil {
				errs = append(errs, fmt.Errorf("cut off %s: %w", acct, err))
				e.Alert(ctx, fmt.Sprintf("guardrail: could not cut off %s", acct), alerttext.WithError(fmt.Sprintf("The %s pack tried to cut off account %s (%s) and failed, so its AI access is not blocked. It tries again on the next run.", run.Name, acct, c.Detail), err))
				continue
			}
		}
		if err := e.Store.PutCutoff(ctx, *c); err != nil {
			errs = append(errs, err)
		}
		kind, verb := KindCutoff, "cut off"
		if !enforce {
			kind, verb = KindWouldCutoff, "would be cut off (dry-run)"
		}
		ev := map[string]any{"cutoff_id": c.ID, "pack": run.Name, "account": acct, "rule": c.Rule, "detail": c.Detail, "cap_usd": c.CapUSD}
		for k, v := range run.Detail {
			ev[k] = v
		}
		e.Emit(ctx, kind, c.ID, "account:"+acct, ev)
		e.Alert(ctx, fmt.Sprintf("guardrail: %s %s", acct, verb), fmt.Sprintf("The %s pack: account %s %s. Rule %s: %s. Cutoff id %s; to lift it, add it to the config's lifts and approve.", run.Name, acct, verb, c.Rule, c.Detail, c.ID))
	}
	return errors.Join(errs...)
}

// lifts detaches cutoffs named in the config's lifts, or whose cap has since
// been raised.
func (e *Engine) lifts(ctx context.Context, run PackRun, now time.Time) error {
	cuts, err := e.Store.Cutoffs(ctx, run.Name)
	if err != nil {
		return fmt.Errorf("read cutoffs: %w", err)
	}
	var errs []error
	for _, c := range cuts {
		reason := ""
		switch {
		case run.Lifts[c.ID]:
			reason = "lift approved"
		case c.CapUSD > 0:
			caps, _ := run.CapsFor(c.Account)
			cur := map[string]float64{"hourly_cap": caps.Hourly, "daily_cap": caps.Daily, "monthly_cap": caps.Monthly}[c.Rule]
			if cur == 0 || cur > c.CapUSD {
				reason = "cap raised"
			}
		case !c.Attached && now.Sub(c.At) >= time.Hour:
			// A dry-run record only dedupes reports; let it expire.
			if err := e.Store.DeleteCutoff(ctx, run.Name, c.Account); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if reason == "" {
			continue
		}
		if c.Attached {
			if err := e.Policies.Detach(ctx, run.PolicyID, c.Account); err != nil {
				errs = append(errs, fmt.Errorf("lift %s: %w", c.ID, err))
				continue
			}
		}
		if err := e.Store.DeleteCutoff(ctx, run.Name, c.Account); err != nil {
			errs = append(errs, err)
			continue
		}
		if c.Attached {
			e.Emit(ctx, KindLift, KindLift+":"+c.ID, "account:"+c.Account, map[string]any{"cutoff_id": c.ID, "pack": run.Name, "account": c.Account, "reason": reason})
			e.Alert(ctx, fmt.Sprintf("guardrail: %s cutoff lifted", c.Account), fmt.Sprintf("The %s pack lifted cutoff %s on account %s: %s.", run.Name, c.ID, c.Account, reason))
		}
	}
	return errors.Join(errs...)
}

// SummaryDelay is how long after an hour ends its summaries are written, so
// trail files that land late are counted.
const SummaryDelay = 10 * time.Minute

func (e *Engine) summaries(ctx context.Context, run PackRun, now time.Time) error {
	hour := now.Add(-SummaryDelay).Truncate(time.Hour).Add(-time.Hour)
	var last time.Time
	name := "summary." + run.Name
	if _, err := e.Store.GetMeta(ctx, name, &last); err != nil {
		return err
	}
	if !last.Before(hour) {
		return nil
	}
	sums, err := e.Store.HourSummaries(ctx, run.Name, hour)
	if err != nil {
		return err
	}
	for _, s := range sums {
		e.Emit(ctx, KindUsage, fmt.Sprintf("%s:%s:%s:%s:%s", KindUsage, run.Name, s.Account, s.Model, hour.Format("2006010215")), "account:"+s.Account,
			map[string]any{"pack": run.Name, "account": s.Account, "model": s.Model, "hour": hour.Format(time.RFC3339), "calls": s.Calls, "input_tokens": s.In, "output_tokens": s.Out, "estimated_usd": s.USD})
	}
	return e.Store.PutMeta(ctx, name, hour)
}

type stallState struct {
	LastFile time.Time `json:"last_file"`
	Stalled  bool      `json:"stalled"`
}

func (e *Engine) trackStall(ctx context.Context, now time.Time, readErr error) {
	var st stallState
	if ok, _ := e.Store.GetMeta(ctx, "stall", &st); !ok {
		st.LastFile = now // first run: start the clock now
	}
	if t := e.Reader.LastFile(); !t.IsZero() && t.After(st.LastFile) {
		st.LastFile = t
	}
	switch {
	case st.Stalled && now.Sub(st.LastFile) < StallAfter && readErr == nil:
		st.Stalled = false
		e.Emit(ctx, KindTrailResume, KindTrailResume+":"+now.Format("20060102T1504"), "", map[string]any{"at": now.Format(time.RFC3339)})
		e.Alert(ctx, "guardrail: trail resumed", "New CloudTrail files are arriving again; the fast packs are watching spend.")
	case !st.Stalled && (now.Sub(st.LastFile) >= StallAfter || isAccessError(readErr)):
		st.Stalled = true
		cause := fmt.Sprintf("no new file since %s", st.LastFile.Format(time.RFC3339))
		if isAccessError(readErr) {
			cause = "the guard can't read the trail: " + readErr.Error()
		}
		e.Emit(ctx, KindTrailStall, KindTrailStall+":"+now.Format("20060102T1504"), "", map[string]any{"cause": cause, "last_file": st.LastFile.Format(time.RFC3339)})
		e.Alert(ctx, "guardrail: trail stalled", "The organization trail is not being read ("+cause+"). AI spend is not being watched until it resumes; caps and totals are kept.")
	}
	_ = e.Store.PutMeta(ctx, "stall", st)
}

type countingState struct {
	Failing bool      `json:"failing"`
	Since   time.Time `json:"since,omitempty"`
}

// trackCounting alerts once when the engine can't store priced calls, and
// again when it can.
func (e *Engine) trackCounting(ctx context.Context, now time.Time, usageErr error) {
	var st countingState
	if _, err := e.Store.GetMeta(ctx, "counting", &st); err != nil {
		return
	}
	switch {
	case usageErr != nil && !st.Failing:
		st = countingState{Failing: true, Since: now}
		e.Alert(ctx, "guardrail: AI spend not counted", alerttext.WithError("The guard reads the trail but can't store the spend of the calls in it, so caps are not being checked against new calls. It reads the same files again on every run until storing works, so no call is lost; a call may be counted twice, which can only cut off sooner.", usageErr))
	case usageErr == nil && st.Failing:
		e.Alert(ctx, "guardrail: AI spend counted again", fmt.Sprintf("The guard stores the spend of new calls again, after failing since %s. The calls from that time are counted now.", st.Since.Format(time.RFC3339)))
		st = countingState{}
	default:
		return
	}
	_ = e.Store.PutMeta(ctx, "counting", st)
}

// isAccessError reports whether a read failed for lack of permission, as when
// a bucket or key policy changes.
func isAccessError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "AccessDenied") || strings.Contains(s, "KMS") && strings.Contains(s, "denied")
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
