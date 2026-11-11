package guard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
)

// AWS Budgets zeroes a budget's calculated spend when the budget is created or
// updated, and can take about a day to recalculate. Its action can't freeze in
// that window, even for a scope already over its new threshold. So after the
// guard changes a budget, it watches the scope's spend in Cost Explorer until
// Budgets catches up (fix-budget-spend-reset).
const (
	// SpendWatchFor is the longest a changed budget is watched.
	SpendWatchFor = 48 * time.Hour
	// SpendCheckEvery spaces Cost Explorer reads for one watched budget.
	SpendCheckEvery = time.Hour

	// Watches are kept as reported records keyed by budget name: when the
	// watch started, and when Cost Explorer was last read for it.
	reportedSpendWatch = "spend_watch"
	reportedSpendCheck = "spend_check"
)

// startSpendWatch starts (or restarts) the watch on a budget the guard just
// created or updated. The first check runs on the same run.
func (g *Guard) startSpendWatch(ctx context.Context, name string) {
	if g.Spend == nil {
		return
	}
	_ = g.State.MarkReported(ctx, reportedSpendWatch, name, g.now())
	_ = g.State.ClearReported(ctx, reportedSpendCheck, name)
}

func (g *Guard) endSpendWatch(ctx context.Context, name string) {
	_ = g.State.ClearReported(ctx, reportedSpendWatch, name)
	_ = g.State.ClearReported(ctx, reportedSpendCheck, name)
}

// watchSpend checks each watched budget at most once an hour, and freezes a
// scope that Cost Explorer shows at or over its threshold (would_freeze in
// dry-run). existing is the budgets as read at runStart, before this run
// updated any of them.
func (g *Guard) watchSpend(ctx context.Context, eff *config.Resolved, want []BudgetSpec, existing map[string]BudgetStatus, runStart time.Time) error {
	if g.Spend == nil {
		return nil
	}
	watches, err := g.State.Reported(ctx, reportedSpendWatch)
	if err != nil || len(watches) == 0 {
		return err
	}
	checked, err := g.State.Reported(ctx, reportedSpendCheck)
	if err != nil {
		return err
	}
	frozen, err := g.State.ListFrozen(ctx)
	if err != nil {
		return fmt.Errorf("list frozen: %w", err)
	}
	isFrozen := map[config.Target]bool{}
	for _, f := range frozen {
		isFrozen[f.Target] = true
	}
	specs := map[string]BudgetSpec{}
	for _, w := range want {
		specs[w.Name] = w
	}
	scopes := map[config.Target]config.ResolvedScope{}
	for _, s := range eff.Scopes {
		scopes[s.Target] = s
	}
	var version int64
	if st, err := g.State.Load(ctx); err == nil {
		version = st.AppliedVersion
	}

	now := g.now()
	var errs []error
	for name, since := range watches {
		w, ok := specs[name]
		scope, inEff := scopes[w.Target]
		switch {
		case !ok || !inEff:
			g.endSpendWatch(ctx, name) // budget gone
			continue
		case now.Sub(since) >= SpendWatchFor:
			g.endSpendWatch(ctx, name)
			continue
		case PeriodStart(w.Period, now).After(since):
			g.endSpendWatch(ctx, name) // a new period: Budgets starts over anyway
			continue
		case since.Before(runStart) && existing[name].ActualUSD > 0:
			// Read after the watch started, so Budgets has recalculated.
			g.endSpendWatch(ctx, name)
			continue
		}
		if isFrozen[w.Target] {
			continue
		}
		if last, ok := checked[name]; ok && now.Sub(last) < SpendCheckEvery {
			continue
		}
		_ = g.State.MarkReported(ctx, reportedSpendCheck, name, now)
		actual, ok := g.spendFor(ctx, w, "watch")
		if !ok {
			continue
		}
		threshold := w.LimitUSD * float64(w.ThresholdPct) / 100
		if actual < threshold {
			continue
		}
		if w.Enforce {
			if err := g.Budgets.Freeze(ctx, w); err != nil {
				errs = append(errs, fmt.Errorf("freeze %s: %w", w.Target, err))
				continue
			}
		}
		if err := g.recordFreeze(ctx, scope, version, actual, HowGuardAttach, SpendSourceCostExplorer); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
