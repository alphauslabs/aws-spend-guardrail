package guard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// PeriodStart returns the start of the budget period containing t.
func PeriodStart(period string, t time.Time) time.Time {
	t = t.UTC()
	switch period {
	case config.PeriodQuarterly:
		m := ((int(t.Month())-1)/3)*3 + 1
		return time.Date(t.Year(), time.Month(m), 1, 0, 0, 0, 0, time.UTC)
	case config.PeriodAnnually:
		return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	default:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
}

// OnThreshold handles a budget notification that a guard-owned budget reached
// its threshold. In enforce mode Budgets has attached the freeze policy on its
// own; the guard records it and alerts. In dry-run nothing was attached and
// the guard records would_freeze.
func (g *Guard) OnThreshold(ctx context.Context, budgetName string) error {
	st, err := g.State.Load(ctx)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	if st.Effective == nil {
		return nil
	}
	var scope *config.ResolvedScope
	for _, s := range st.Effective.Scopes {
		if BudgetName(s.Target) == budgetName {
			s := s
			scope = &s
			break
		}
	}
	if scope == nil {
		return nil // not a scope the guard knows; ignore
	}
	var actual float64
	owned, err := g.Budgets.ListOwned(ctx)
	if err == nil {
		for _, b := range owned {
			if b.Spec.Name == budgetName {
				actual = b.ActualUSD
			}
		}
	}
	if scope.Mode == config.ModeEnforce {
		// The guard may have attached the policy already while Budgets had
		// no spend for this budget; the action finding it attached is not a
		// second freeze.
		if frozen, err := g.State.ListFrozen(ctx); err == nil {
			for _, f := range frozen {
				if f.Target == scope.Target {
					return nil
				}
			}
		}
	}
	return g.recordFreeze(ctx, *scope, st.AppliedVersion, actual, HowBudgetAction, SpendSourceBudget)
}

// recordFreeze records a scope reaching its threshold: in enforce, the freeze
// (state, event, and local alert); in dry-run, would_freeze. how says what
// attached the policy, and source where actual came from.
func (g *Guard) recordFreeze(ctx context.Context, scope config.ResolvedScope, version int64, actual float64, how, source string) error {
	now := g.now()
	period := PeriodStart(scope.Period, now)
	detail := map[string]any{
		"ceiling_usd":   scope.CeilingUSD,
		"threshold_pct": scope.ThresholdPct,
		"actual_usd":    actual,
		"freeze_style":  scope.FreezeStyle,
		"period_start":  period.Format("2006-01-02"),
		"spend_source":  source,
	}
	idFor := func(kind string) string {
		return kind + ":" + scope.Target.String() + ":" + period.Format("2006-01-02")
	}
	if scope.Mode != config.ModeEnforce {
		g.emit(ctx, schema.KindWouldFreeze, idFor(schema.KindWouldFreeze), scope.Target.String(), version, detail)
		return nil
	}
	detail["how"] = how
	if err := g.State.MarkFrozen(ctx, Frozen{
		Target:       scope.Target,
		Since:        now,
		PeriodStart:  period,
		PolicyID:     g.policyFor(scope.FreezeStyle),
		ThresholdUSD: scope.CeilingUSD * float64(scope.ThresholdPct) / 100,
	}); err != nil {
		return fmt.Errorf("record freeze: %w", err)
	}
	g.emit(ctx, schema.KindFreeze, idFor(schema.KindFreeze), scope.Target.String(), version, detail)
	g.alert(ctx, "guardrail: spend frozen", fmt.Sprintf(
		"%s is frozen: actual cost %.2f USD reached %d%% of its %.2f USD ceiling (freeze style %s). New spend is blocked; nothing running was stopped.",
		scope.Target, actual, scope.ThresholdPct, scope.CeilingUSD, scope.FreezeStyle))
	return nil
}

// Daily runs once a day: lift frozen scopes at a new period when configured,
// report drift between recorded freezes and actual policy attachments, and
// write the heartbeat. OU membership is refreshed by the reconcile that
// follows.
func (g *Guard) Daily(ctx context.Context) error {
	st, err := g.State.Load(ctx)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	var errs []error
	if st.Effective != nil {
		if err := g.periodLifts(ctx, st.Effective); err != nil {
			errs = append(errs, err)
		}
	}
	if err := g.Drift(ctx); err != nil {
		errs = append(errs, err)
	}
	if st.Effective != nil {
		if err := g.coverage(ctx, st.Effective, st.AppliedVersion); err != nil {
			errs = append(errs, err)
		}
	}
	hb := map[string]any{"applied_version": st.AppliedVersion, "effective_version": st.EffectiveVersion, "pending": st.Pending != nil}
	if keys := g.checkKeys(ctx, st); keys != nil {
		hb["keys"] = keys
		if err := g.State.Save(ctx, st); err != nil {
			errs = append(errs, fmt.Errorf("save state: %w", err))
		}
	}
	day := g.now().Format("2006-01-02")
	g.emit(ctx, schema.KindHeartbeat, schema.KindHeartbeat+":"+day, "", st.AppliedVersion, g.selfReport(hb))
	if st.Effective != nil {
		if err := g.Reconcile(ctx, st.Effective); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (g *Guard) periodLifts(ctx context.Context, eff *config.Resolved) error {
	frozen, err := g.State.ListFrozen(ctx)
	if err != nil {
		return err
	}
	specs, err := g.Specs(ctx, eff)
	if err != nil {
		return err
	}
	byTarget := map[config.Target]BudgetSpec{}
	for _, s := range specs {
		byTarget[s.Target] = s
	}
	var errs []error
	for _, f := range frozen {
		sc, ok := eff.Scope(f.Target)
		if !ok || sc.MonthEnd != config.MonthEndAutoLift {
			continue
		}
		if !PeriodStart(sc.Period, g.now()).After(f.PeriodStart) {
			continue
		}
		if err := g.lift(ctx, f, byTarget[f.Target], "period_start", nil); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Drift compares recorded freezes with the freeze policies' actual
// attachments. Differences are recorded and alerted; a freeze someone
// detached is never re-attached.
func (g *Guard) Drift(ctx context.Context) error {
	frozen, err := g.State.ListFrozen(ctx)
	if err != nil {
		return err
	}
	attached := map[config.Target]bool{}
	for _, p := range []string{g.S.PolicyA, g.S.PolicyB} {
		ts, err := g.Org.PolicyTargets(ctx, p)
		if err != nil {
			return fmt.Errorf("list targets of %s: %w", p, err)
		}
		for _, t := range ts {
			attached[t] = true
		}
	}
	recorded := map[config.Target]bool{}
	day := g.now().Format("2006-01-02")
	for _, f := range frozen {
		recorded[f.Target] = true
		if attached[f.Target] {
			continue
		}
		g.emit(ctx, schema.KindDrift, fmt.Sprintf("drift:%s:detached:%d", f.Target, f.Since.Unix()), f.Target.String(), 0,
			map[string]any{"what": "freeze policy detached outside the guard", "frozen_since": f.Since})
		g.alert(ctx, "guardrail: drift", fmt.Sprintf("%s was frozen by the guard but its freeze policy is no longer attached. The guard does not re-attach it.", f.Target))
		if err := g.State.ClearFrozen(ctx, f.Target); err != nil {
			return err
		}
	}
	for t := range attached {
		if recorded[t] {
			continue
		}
		g.emit(ctx, schema.KindDrift, fmt.Sprintf("drift:%s:attached:%s", t, day), t.String(), 0,
			map[string]any{"what": "freeze policy attached without a recorded freeze"})
		g.alert(ctx, "guardrail: drift", fmt.Sprintf("a freeze policy is attached to %s, but the guard recorded no freeze for it.", t))
	}
	return nil
}
