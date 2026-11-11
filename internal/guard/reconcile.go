package guard

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// BudgetPrefix starts the name of every budget the guard owns.
const BudgetPrefix = "guardrail-"

// BudgetName is the name of the budget for a target.
func BudgetName(t config.Target) string { return BudgetPrefix + t.Kind + "-" + t.ID }

// Specs turns an effective config into the budgets that should exist, with OU
// membership read from the organization.
func (g *Guard) Specs(ctx context.Context, eff *config.Resolved) ([]BudgetSpec, error) {
	var out []BudgetSpec
	for _, s := range eff.Scopes {
		accounts := []string{s.Target.ID}
		if s.Target.Kind == "ou" {
			var err error
			accounts, err = g.Org.AccountsInOU(ctx, s.Target.ID)
			if err != nil {
				return nil, fmt.Errorf("list accounts in %s: %w", s.Target.ID, err)
			}
			sort.Strings(accounts)
		}
		policy := ""
		if s.Mode == config.ModeEnforce {
			policy = g.policyFor(s.FreezeStyle) // only enforcing scopes get an action
		}
		out = append(out, BudgetSpec{
			Name:         BudgetName(s.Target),
			Target:       s.Target,
			Accounts:     accounts,
			LimitUSD:     s.CeilingUSD,
			Period:       s.Period,
			ThresholdPct: s.ThresholdPct,
			PolicyID:     policy,
			Enforce:      s.Mode == config.ModeEnforce,
		})
	}
	return out, nil
}

// Reconcile makes guard-owned budgets match the effective config, then lifts
// freezes whose budget was raised above current spend. Budgets the guard
// doesn't own are never touched.
func (g *Guard) Reconcile(ctx context.Context, eff *config.Resolved) error {
	want, err := g.Specs(ctx, eff)
	if err != nil {
		return err
	}
	runStart := g.now() // budgets read from here on may be after a watch began
	have, err := g.Budgets.ListOwned(ctx)
	if err != nil {
		return fmt.Errorf("list budgets: %w", err)
	}
	existing := map[string]BudgetStatus{}
	for _, b := range have {
		existing[b.Spec.Name] = b
	}
	var errs []error
	wanted := map[string]bool{}
	for _, w := range want {
		wanted[w.Name] = true
		if e, ok := existing[w.Name]; ok && sameSpec(e.Spec, w) {
			continue
		}
		if err := g.Budgets.Put(ctx, w); err != nil {
			errs = append(errs, fmt.Errorf("put %s: %w", w.Name, err))
			continue
		}
		g.startSpendWatch(ctx, w.Name)
	}
	frozen, err := g.State.ListFrozen(ctx)
	if err != nil {
		return fmt.Errorf("list frozen: %w", err)
	}
	for name, e := range existing {
		if !wanted[name] {
			// A removed scope (an approved loosening) is lifted before its
			// budget goes, so no freeze is left without a budget behind it.
			for _, f := range frozen {
				if f.Target == e.Spec.Target {
					if err := g.lift(ctx, f, e.Spec, "scope_removed", nil); err != nil {
						errs = append(errs, err)
					}
				}
			}
			if err := g.Budgets.Delete(ctx, name); err != nil {
				errs = append(errs, fmt.Errorf("delete %s: %w", name, err))
			}
		}
	}
	if err := g.liftRaised(ctx, want, existing); err != nil {
		errs = append(errs, err)
	}
	if err := g.watchSpend(ctx, eff, want, existing, runStart); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		err := errors.Join(errs...)
		g.alert(ctx, "guardrail: reconcile incomplete", err.Error())
		return err
	}
	return nil
}

func sameSpec(a, b BudgetSpec) bool {
	return a.Name == b.Name && a.Target == b.Target && a.LimitUSD == b.LimitUSD &&
		a.Period == b.Period && a.ThresholdPct == b.ThresholdPct && a.PolicyID == b.PolicyID &&
		a.Enforce == b.Enforce && strings.Join(a.Accounts, ",") == strings.Join(b.Accounts, ",")
}

// liftRaised lifts a frozen scope whose threshold was raised above both its
// threshold at freeze time and current spend. Spend comes from Cost Explorer,
// not the budget: updating a budget zeroes its spend until AWS recalculates,
// so the budget's figure would lift a freeze that is still over the new
// threshold on every run after the raise.
func (g *Guard) liftRaised(ctx context.Context, want []BudgetSpec, _ map[string]BudgetStatus) error {
	frozen, err := g.State.ListFrozen(ctx)
	if err != nil {
		return fmt.Errorf("list frozen: %w", err)
	}
	specs := map[config.Target]BudgetSpec{}
	for _, w := range want {
		specs[w.Target] = w
	}
	var errs []error
	for _, f := range frozen {
		w, ok := specs[f.Target]
		if !ok {
			continue // scope removed: lifted with its budget above
		}
		threshold := w.LimitUSD * float64(w.ThresholdPct) / 100
		if threshold <= f.ThresholdUSD {
			continue // not raised: no spend read needed
		}
		actual, ok := g.spendFor(ctx, w, "lift")
		if !ok || actual >= threshold {
			continue
		}
		if err := g.lift(ctx, f, w, "budget_raised", map[string]any{
			"actual_usd": actual, "threshold_usd": threshold, "spend_source": SpendSourceCostExplorer,
		}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (g *Guard) lift(ctx context.Context, f Frozen, w BudgetSpec, reason string, detail map[string]any) error {
	how, err := g.Budgets.Release(ctx, w)
	if err != nil {
		return fmt.Errorf("lift %s: %w", f.Target, err)
	}
	if err := g.State.ClearFrozen(ctx, f.Target); err != nil {
		return fmt.Errorf("clear frozen %s: %w", f.Target, err)
	}
	if detail == nil {
		detail = map[string]any{}
	}
	detail["reason"] = reason
	detail["how"] = how
	detail["frozen_since"] = f.Since
	g.emit(ctx, schema.KindLift, fmt.Sprintf("%s:%s:%d", schema.KindLift, f.Target, f.Since.Unix()), f.Target.String(), 0, detail)
	g.alert(ctx, "guardrail: freeze lifted", fmt.Sprintf("%s was lifted (%s)", f.Target, reason))
	return nil
}

// Teardown runs when the stack is deleted: lift every recorded freeze, then
// delete every guard-owned budget. Budgets the guard doesn't own are left.
func (g *Guard) Teardown(ctx context.Context) error {
	have, err := g.Budgets.ListOwned(ctx)
	if err != nil {
		return fmt.Errorf("list budgets: %w", err)
	}
	frozen, err := g.State.ListFrozen(ctx)
	if err != nil {
		return fmt.Errorf("list frozen: %w", err)
	}
	byTarget := map[config.Target]BudgetSpec{}
	for _, b := range have {
		byTarget[b.Spec.Target] = b.Spec
	}
	var errs []error
	for _, f := range frozen {
		spec, ok := byTarget[f.Target]
		if !ok {
			spec = BudgetSpec{Name: BudgetName(f.Target), Target: f.Target, PolicyID: f.PolicyID}
		}
		if err := g.lift(ctx, f, spec, "stack_deleted", nil); err != nil {
			errs = append(errs, err)
		}
	}
	for _, b := range have {
		if err := g.Budgets.Delete(ctx, b.Spec.Name); err != nil {
			errs = append(errs, fmt.Errorf("delete %s: %w", b.Spec.Name, err))
		}
	}
	if err := g.teardownAI(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// teardownAI detaches the AI cutoff policy from every account it is attached
// to, as AWS reports it, so the stack can delete the policy even if the
// guard's own records were lost.
func (g *Guard) teardownAI(ctx context.Context) error {
	if g.Fast == nil || g.Fast.AIPolicyID == "" {
		return nil
	}
	targets, err := g.Org.PolicyTargets(ctx, g.Fast.AIPolicyID)
	if err != nil {
		return fmt.Errorf("list AI cutoff targets: %w", err)
	}
	var errs []error
	for _, t := range targets {
		if err := g.Fast.Engine.Policies.Detach(ctx, g.Fast.AIPolicyID, t.ID); err != nil {
			errs = append(errs, fmt.Errorf("detach AI cutoff from %s: %w", t.ID, err))
			continue
		}
		_ = g.Fast.Engine.Store.DeleteCutoff(ctx, "ai", t.ID)
		g.alert(ctx, fmt.Sprintf("guardrail: %s cutoff lifted", t.ID), "The AI cutoff on account "+t.ID+" was lifted because the guard's stack is being deleted.")
	}
	return errors.Join(errs...)
}
