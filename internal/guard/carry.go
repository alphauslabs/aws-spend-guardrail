package guard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// When the guard updates a budget whose freeze action has executed, AWS
// resets the action, and the reset detaches the freeze policy about a second
// later. The guard waits for the reset to finish and attaches the policy
// again, so a config change never unfreezes a scope it doesn't lift
// (fix-action-reset-unfreeze).
const (
	// CarryWait is how long one run waits for AWS's reset to finish.
	CarryWait = 30 * time.Second
	// carryPoll spaces the action status reads while waiting.
	carryPoll = 2 * time.Second

	// reportedCarry marks a budget whose reset outlasted a run's wait, so a
	// later run attaches the policy once the reset is done.
	reportedCarry = "carry_freeze"

	actionStandby = "STANDBY"
)

func (g *Guard) sleep(d time.Duration) {
	if g.Sleep != nil {
		g.Sleep(d)
		return
	}
	time.Sleep(d)
}

// carryFreezes attaches the freeze policy again for each budget that was
// updated with its action executed (now), and for each left by an earlier
// run's timeout, as long as its target is still recorded frozen.
func (g *Guard) carryFreezes(ctx context.Context, now []BudgetSpec, want []BudgetSpec) error {
	pending, err := g.State.Reported(ctx, reportedCarry)
	if err != nil {
		return err
	}
	if len(now) == 0 && len(pending) == 0 {
		return nil
	}
	frozen, err := g.State.ListFrozen(ctx)
	if err != nil {
		return fmt.Errorf("list frozen: %w", err)
	}
	byTarget := map[config.Target]Frozen{}
	for _, f := range frozen {
		byTarget[f.Target] = f
	}
	specs := map[string]BudgetSpec{}
	for _, w := range want {
		specs[w.Name] = w
	}
	var version int64
	if st, err := g.State.Load(ctx); err == nil {
		version = st.AppliedVersion
	}

	var errs []error
	carry := func(w BudgetSpec, wait bool, what string) {
		f, ok := byTarget[w.Target]
		if !ok || !w.Enforce {
			_ = g.State.ClearReported(ctx, reportedCarry, w.Name) // lifted, or no longer enforcing
			return
		}
		done, err := g.resetDone(ctx, w.Name, wait)
		if err != nil {
			errs = append(errs, fmt.Errorf("carry freeze of %s: %w", w.Target, err))
			_ = g.State.MarkReported(ctx, reportedCarry, w.Name, g.now())
			return
		}
		if !done {
			_ = g.State.MarkReported(ctx, reportedCarry, w.Name, g.now())
			return
		}
		if err := g.Budgets.Freeze(ctx, w); err != nil {
			errs = append(errs, fmt.Errorf("carry freeze of %s: %w", w.Target, err))
			_ = g.State.MarkReported(ctx, reportedCarry, w.Name, g.now())
			return
		}
		_ = g.State.ClearReported(ctx, reportedCarry, w.Name)
		at := g.now()
		g.emit(ctx, schema.KindFreezeCarried,
			fmt.Sprintf("%s:%s:%d:%d", schema.KindFreezeCarried, w.Target, f.Since.Unix(), at.Unix()), w.Target.String(), version,
			map[string]any{
				"frozen_since": f.Since,
				"budget":       w.Name,
				"what":         what,
			})
	}
	seen := map[string]bool{}
	for _, w := range now {
		seen[w.Name] = true
		carry(w, true, "AWS reset the budget's executed freeze action when the guard updated the budget, which detached the freeze policy; the guard attached it again")
	}
	for name := range pending {
		if seen[name] {
			continue
		}
		w, ok := specs[name]
		if !ok {
			_ = g.State.ClearReported(ctx, reportedCarry, name) // budget gone
			continue
		}
		carry(w, false, "AWS reset the budget's executed freeze action, which detached the freeze policy; the guard attached it again once the reset finished")
	}
	return errors.Join(errs...)
}

// resetDone reports whether AWS's reset of the budget's action has finished
// (the action is in standby). With wait, it reads the status every carryPoll
// for up to CarryWait: right after the update the action can still read as
// executed, because the reset starts asynchronously.
func (g *Guard) resetDone(ctx context.Context, name string, wait bool) (bool, error) {
	reads := 1
	if wait {
		reads = int(CarryWait / carryPoll)
	}
	for i := 0; i < reads; i++ {
		if i > 0 {
			g.sleep(carryPoll)
		}
		st, err := g.Budgets.ActionStatus(ctx, name)
		if err != nil {
			return false, err
		}
		if st == actionStandby {
			return true, nil
		}
	}
	return false, nil
}

// actionResetting is AWS's reset still running.
const actionResetting = "RESET_IN_PROGRESS"

// attachedTargets lists every target a freeze policy is attached to now.
func (g *Guard) attachedTargets(ctx context.Context) (map[config.Target]bool, error) {
	attached := map[config.Target]bool{}
	for _, p := range []string{g.S.PolicyA, g.S.PolicyB} {
		ts, err := g.Org.PolicyTargets(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("list targets of %s: %w", p, err)
		}
		for _, t := range ts {
			attached[t] = true
		}
	}
	return attached, nil
}

// inNewPeriod is a recorded freeze of a scope that keeps its freezes across
// periods (not auto_lift) whose budget period has started since it froze.
func inNewPeriod(f Frozen, sc config.ResolvedScope, now time.Time) bool {
	return sc.MonthEnd != config.MonthEndAutoLift && PeriodStart(sc.Period, now).After(f.PeriodStart)
}

// carryPeriodResets keeps stay_frozen freezes in force across a new budget
// period (fix-period-reset-unfreeze, design D1). If AWS resets an executed
// action when a period starts, as it does when a budget is updated, the reset
// detaches the freeze policy; the guard attaches it again. A reset still running
// is marked for the carry's pending path. A policy detached while the action
// still reads as executed was detached by a person, and is left to Drift.
func (g *Guard) carryPeriodResets(ctx context.Context, eff *config.Resolved) error {
	if eff == nil {
		return nil
	}
	frozen, err := g.State.ListFrozen(ctx)
	if err != nil || len(frozen) == 0 {
		return err
	}
	scopes := map[config.Target]config.ResolvedScope{}
	for _, s := range eff.Scopes {
		scopes[s.Target] = s
	}
	now := g.now()
	var due []Frozen
	for _, f := range frozen {
		if sc, ok := scopes[f.Target]; ok && sc.Mode == config.ModeEnforce && inNewPeriod(f, sc, now) {
			due = append(due, f)
		}
	}
	if len(due) == 0 {
		return nil
	}
	attached, err := g.attachedTargets(ctx)
	if err != nil {
		return err
	}
	specs, err := g.Specs(ctx, eff)
	if err != nil {
		return err
	}
	byTarget := map[config.Target]BudgetSpec{}
	for _, w := range specs {
		byTarget[w.Target] = w
	}
	var version int64
	if st, err := g.State.Load(ctx); err == nil {
		version = st.AppliedVersion
	}
	var errs []error
	for _, f := range due {
		w, ok := byTarget[f.Target]
		if attached[f.Target] || !ok || !w.Enforce {
			continue
		}
		status, err := g.Budgets.ActionStatus(ctx, w.Name)
		if err != nil {
			errs = append(errs, fmt.Errorf("period reset of %s: %w", f.Target, err))
			continue
		}
		switch status {
		case actionResetting:
			_ = g.State.MarkReported(ctx, reportedCarry, w.Name, now)
		case actionStandby:
			if err := g.Budgets.Freeze(ctx, w); err != nil {
				errs = append(errs, fmt.Errorf("period reset of %s: %w", f.Target, err))
				_ = g.State.MarkReported(ctx, reportedCarry, w.Name, now)
				continue
			}
			g.emit(ctx, schema.KindFreezeCarried,
				fmt.Sprintf("%s:%s:%d:%d", schema.KindFreezeCarried, w.Target, f.Since.Unix(), now.Unix()), w.Target.String(), version,
				map[string]any{
					"frozen_since": f.Since,
					"budget":       w.Name,
					"reason":       "period_reset",
					"period_start": PeriodStart(scopes[f.Target].Period, now).Format("2006-01-02"),
					"what":         "AWS reset the budget's executed freeze action at the start of a new budget period, which detached the freeze policy; the scope keeps its freeze (stay_frozen), so the guard attached it again",
				})
		}
	}
	return errors.Join(errs...)
}
