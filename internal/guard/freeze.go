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
	// Write a heartbeat if this run changed the guard's state (add-heartbeat-state).
	defer g.heartbeatIfChanged(ctx)
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
	var existing *Frozen
	if scope.Mode == config.ModeEnforce {
		// The guard may have attached the policy already while Budgets had
		// no spend for this budget, or this is a repeated notification: the
		// same freeze, written again under its own id and not alerted.
		if frozen, err := g.State.ListFrozen(ctx); err == nil {
			for _, f := range frozen {
				if f.Target == scope.Target {
					f := f
					existing = &f
				}
			}
		}
	}
	return g.recordFreeze(ctx, *scope, st.AppliedVersion, actual, HowBudgetAction, SpendSourceBudget, existing)
}

// FreezeEventID names one freeze of a scope: its period and when it began, so
// a scope frozen again after a lift in the same period gets its own event,
// while a repeat of the same freeze reuses the id (fix-freeze-event-ids).
func FreezeEventID(f Frozen) string {
	return fmt.Sprintf("%s:%s:%s:%d", schema.KindFreeze, f.Target, f.PeriodStart.Format("2006-01-02"), f.Since.Unix())
}

// recordFreeze records a scope reaching its threshold: in enforce, the freeze
// (state, event, and local alert); in dry-run, would_freeze. how says what
// attached the policy, and source where actual came from. existing is the
// freeze already in force for the scope, if any: its event is written again
// under the same id, which Jennah keeps once, and nothing else happens.
func (g *Guard) recordFreeze(ctx context.Context, scope config.ResolvedScope, version int64, actual float64, how, source string, existing *Frozen) error {
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
	if scope.Mode != config.ModeEnforce {
		// A dry-run scope is never lifted: one per period.
		id := schema.KindWouldFreeze + ":" + scope.Target.String() + ":" + period.Format("2006-01-02")
		g.emit(ctx, schema.KindWouldFreeze, id, scope.Target.String(), version, detail)
		return nil
	}
	detail["how"] = how
	if existing != nil {
		detail["period_start"] = existing.PeriodStart.Format("2006-01-02")
		g.emitAt(ctx, existing.Since, schema.KindFreeze, FreezeEventID(*existing), scope.Target.String(), version, detail)
		return nil
	}
	f := Frozen{
		Target:       scope.Target,
		Since:        now,
		PeriodStart:  period,
		PolicyID:     g.policyFor(scope.FreezeStyle),
		ThresholdUSD: scope.CeilingUSD * float64(scope.ThresholdPct) / 100,
		How:          how,
	}
	if err := g.State.MarkFrozen(ctx, f); err != nil {
		return fmt.Errorf("record freeze: %w", err)
	}
	g.emitAt(ctx, f.Since, schema.KindFreeze, FreezeEventID(f), scope.Target.String(), version, detail)
	g.alert(ctx, "guardrail: spend frozen", fmt.Sprintf(
		"%s is frozen: its actual cost of %.2f USD reached %d%% of its %.2f USD ceiling (freeze style %s). New spend is blocked, and nothing running was stopped.",
		targetName(scope.Target), actual, scope.ThresholdPct, scope.CeilingUSD, scope.FreezeStyle))
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
	keys := g.checkKeys(ctx, st)
	if keys != nil {
		if err := g.State.Save(ctx, st); err != nil {
			errs = append(errs, fmt.Errorf("save state: %w", err))
		}
	}
	// The daily heartbeat always carries the state (add-heartbeat-state); a
	// snapshot that can't be read still leaves the heartbeat as liveness.
	snap, err := g.snapshot(ctx, st)
	if err != nil {
		errs = append(errs, err)
		snap = map[string]any{}
	}
	day := g.now().Format("2006-01-02")
	// Taken before heartbeatDetail adds spend to the snapshot.
	dailyDigest := ""
	if len(snap) > 0 {
		dailyDigest = digest(snap)
	}
	var hbKeys any
	if keys != nil {
		hbKeys = keys
	}
	g.emit(ctx, schema.KindHeartbeat, schema.KindHeartbeat+":"+day, "", st.AppliedVersion, g.heartbeatDetail(ctx, st, snap, HeartbeatDaily, hbKeys))
	if dailyDigest != "" {
		_ = g.State.SetHeartbeatDigest(ctx, dailyDigest)
	}
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
		g.alert(ctx, "guardrail: drift", fmt.Sprintf("%s was frozen by the guard, but its freeze policy is no longer attached, so new spend is no longer blocked. The guard does not re-attach it: publish a config change or attach the policy again if the freeze should stay.", targetName(f.Target)))
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
		g.alert(ctx, "guardrail: drift", fmt.Sprintf("A freeze policy is attached to %s, but the guard recorded no freeze for it. Check who attached it, and detach it if the account shouldn't be frozen.", targetRef(t)))
	}
	return nil
}
