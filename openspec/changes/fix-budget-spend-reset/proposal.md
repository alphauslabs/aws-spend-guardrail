## Why

AWS Budgets resets a budget's calculated spend to zero whenever the budget is created or updated ("When you modify a budget, the calculatedSpend drops to zero until Amazon Web Services has new usage data", from the `UpdateBudget` API reference). The new value can take about a day to come back: in the e2e org, a budget rewritten at 17:20 showed 0.0 until 20:17 the next day, while Cost Explorer showed 5.24 USD for the same account. The e2e lift test then showed the reset again when the guard raised a frozen ceiling.

The guard trusts the budget's spend in two places, and both go wrong in that window:

- **A frozen scope can be lifted while still over its ceiling.** The guard checks every frozen scope on each run and lifts it when spend is below the threshold. If a ceiling is raised but not far enough (frozen at 4 USD with 5.74 spent, raised to 5), the first run correctly keeps the freeze. The budget update has zeroed the spend, so the next run reads 0 and lifts it.
- **A changed budget can't freeze for up to a day.** After a tightening (a lower ceiling, a new scope, or `dry-run` to `enforce`), AWS Budgets can't fire the freeze action until it recalculates spend, even when spend is already over the new threshold. "Tightening applies at once" holds for the budget's settings but not for the freeze.

## What Changes

- Lift decisions for a raised ceiling use spend from Cost Explorer for the scope's accounts, period to date, instead of the budget's calculated spend. If Cost Explorer can't be read, the freeze stays and the guard alerts.
- After the guard creates or updates an enforcing budget, it checks that scope's Cost Explorer spend about once an hour until the budget reports spend again (at most 48 hours). If spend is at or over the threshold, the guard attaches the scope's freeze policy itself, records the freeze, and alerts, as it does for a freeze AWS Budgets makes. A dry-run scope records `would_freeze` instead.
- The guard's role gains `ce:GetCostAndUsage` and `organizations:AttachPolicy` on the two freeze policies (it already has `DetachPolicy` on them).
- Freeze and lift events say where the spend figure came from (`spend_source`: `budget` or `cost_explorer`) and how a freeze was made (`how`: `budget_action` or `guard_attach`).

Out of scope: stopping AWS from zeroing the spend (it can't be set), and replacing AWS Budgets as the primary freeze trigger. Budgets still freezes without the guard or Jennah.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `spend-freeze`: lifts are decided on Cost Explorer spend, and the guard freezes a scope whose budget it just changed when spend is already over the threshold.

## Impact

- `internal/awsx`: a Cost Explorer reader (month, quarter, or year to date, by linked accounts), and attaching a freeze policy.
- `internal/guard`: `liftRaised` uses Cost Explorer spend; reconcile marks changed enforcing budgets for a spend check; a new hourly check freezes when over; state records each pending check.
- `guard/template.yaml`: the IAM additions above; `guard/main.go` wiring.
- Cost: Cost Explorer API calls are billed at 0.01 USD each. Calls happen only for frozen scopes being considered for a lift, and hourly for a changed budget until it reports spend, so a typical org pays cents a month.
- Docs (`docs/operating.md`, `docs/install.md`, `docs/aws-facts.md`) and `CHANGELOG.md`, including Security-relevant.
