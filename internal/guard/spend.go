package guard

import (
	"context"
	"fmt"
)

// Where a freeze or lift took its spend figure from, and how a freeze was
// made (fix-budget-spend-reset).
const (
	SpendSourceBudget       = "budget"
	SpendSourceCostExplorer = "cost_explorer"
	HowBudgetAction         = "budget_action"
	HowGuardAttach          = "guard_attach"
)

// reportedSpendError marks a scope whose Cost Explorer read is failing, so the
// failure is alerted once, not on every run.
const reportedSpendError = "spend_error"

// spendFor reads a scope's period-to-date cost from Cost Explorer. On failure
// it alerts once per scope until a read succeeds, and returns ok false; the
// caller then keeps the safer state (a freeze stays, nothing is lifted).
func (g *Guard) spendFor(ctx context.Context, w BudgetSpec, purpose string) (float64, bool) {
	if g.Spend == nil {
		return 0, false
	}
	now := g.now()
	actual, err := g.Spend.Actual(ctx, w.Accounts, PeriodStart(w.Period, now), now)
	key := w.Target.String()
	reported, _ := g.State.Reported(ctx, reportedSpendError)
	if err != nil {
		if _, done := reported[key]; !done {
			msg := fmt.Sprintf("Cost Explorer could not be read for %s, so the guard can't check its spend: %v.", w.Target, err)
			if purpose == "lift" {
				msg += " Its freeze stays until a read succeeds."
			} else {
				msg += " The AWS Budgets action still freezes it once Budgets has spend for it."
			}
			g.alert(ctx, "guardrail: spend check failing", msg)
			_ = g.State.MarkReported(ctx, reportedSpendError, key, now)
		}
		return 0, false
	}
	if _, done := reported[key]; done {
		_ = g.State.ClearReported(ctx, reportedSpendError, key)
	}
	return actual, true
}
