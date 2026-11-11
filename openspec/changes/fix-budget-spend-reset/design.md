# Design

## Context

- `Budgets.Put` (`internal/awsx/budgets.go`) calls `CreateBudget` or `UpdateBudget` whenever `sameSpec` reports a difference (`internal/guard/reconcile.go`). Either call leaves `CalculatedSpend` at zero until AWS recalculates. AWS documents this for `UpdateBudget`, and `calculatedSpend` can't be set by the caller. In e2e the gap lasted about 27 hours. The first test budget, newly created, also stayed at 0 for hours.
- `liftRaised` reads `existing[...].ActualUSD`, which `ListOwned` takes from the budget before the run's `Put`. That is right on the run that applies the raise, but every later run reads the zeroed value, and the check repeats for as long as the scope is frozen.
- The guard's role has `organizations:DetachPolicy` on the freeze policies (`DetachFreezePolicies`), not `AttachPolicy`. `Release` already falls back to detaching directly when the Budgets action wasn't what attached the policy.
- Cost Explorer (`ce:GetCostAndUsage`, us-east-1 only) answered 5.24 USD for e2estandard03 while every budget said 0. Each call costs 0.01 USD, and its data lags usage by several hours.
- The pull runs every 5 minutes; there is no hourly schedule.

## Goals / Non-Goals

**Goals:**
- No freeze is lifted on a zeroed budget spend.
- A scope whose budget the guard changed can still freeze while the budget reads zero.
- Cost Explorer use stays small and bounded.

**Non-Goals:**
- Replacing the Budgets action as the main freeze trigger. It keeps working without the guard or Jennah.
- Reading Cost Explorer on every run for every scope.
- Avoiding budget updates further. `fix-budget-action-rewrites` already writes only on a real change, and a limit change has to update the budget.

## Decisions

### D1. Lift decisions read Cost Explorer
`liftRaised` first applies the cheap test that needs no spend: the new threshold must be above the threshold at freeze time. Only for a scope that passes does it call `SpendSource.Actual(ctx, accounts, periodStart, now)`, and it lifts only when that cost is below the new threshold. Errors keep the freeze and alert once per state change, reusing the guard's alert helper. The event records `actual_usd` and `spend_source: cost_explorer`.

Cost Explorer is queried with `UnblendedCost` and a `LINKED_ACCOUNT` filter on the scope's accounts (an OU scope's accounts, as for its budget), over `[period start, tomorrow)` with `MONTHLY` granularity, summing the results. No record-type filter, matching the budget's default cost types (credits and refunds included).

*Alternative:* remember the last spend read before each update and never use a lower figure in the same period. It needs no new permission, but it leaves the tightening gap open, and a remembered figure goes stale while spend grows.

### D2. A spend watch per changed budget, checked hourly from the pull
When `Put` creates or updates a budget, reconcile records a watch for it (`Since`), and on each check the time of that check (`LastChecked`). Both are kept as reported records in the state table (kinds `spend_watch` and `spend_check`, keyed by budget name), like `unprotected_account`, so reconcile doesn't have to load and save the whole config state. On each pull, for each watched budget whose `LastChecked` is an hour old or more:
1. Stop watching if the budget, read at the start of a run that began after the watch did, reports nonzero spend, 48 hours have passed, the budget is gone, or a new period has started since `Since`.
2. Skip it if the scope is already frozen (recorded frozen, or its freeze policy is attached).
3. Read Cost Explorer. At or over the threshold: in `enforce`, attach the scope's freeze policy and record the freeze as `OnThreshold` does (`MarkFrozen`, `freeze` event, local alert) with `how: guard_attach` and `spend_source: cost_explorer`. In `dry-run`, emit `would_freeze` (its id is per period, so it is written once).
4. Set `LastChecked`.

The first check runs on the same pull as the update, so a tightening below current spend freezes within minutes, as far as Cost Explorer's own lag allows. Hourly after that keeps the cost at about 0.24 USD a day per watched scope at most, and only during the window.

*Alternative:* an hourly EventBridge schedule. Not needed, because the pull already runs often enough to rate-limit from state.

### D3. Attaching the freeze policy
`Budgets.Freeze(ctx, spec)` attaches `spec.PolicyID` to `spec.Target.ID` and treats "already attached" (`DuplicatePolicyAttachmentException`) as success. When the Budgets action later fires on its own, it finds the policy already attached, which is harmless. A lift still goes through `Release`, which reverses the action if it executed, or else detaches directly. That covers both ways a freeze was made.

IAM: add `organizations:AttachPolicy` to the guard role's freeze-policy statement, with the same resources as `DetachPolicy` (the two freeze policies, accounts, and OUs), renamed `AttachDetachFreezePolicies`. Add `ce:GetCostAndUsage` on `*`, since Cost Explorer has no resource-level permissions.

### D4. Event fields
Freeze, would_freeze, and lift events gain `spend_source`, and freeze events gain `how`. `OnThreshold` sets `budget_action` and `budget`. The console and the inbox schema treat `detail` as free-form JSON, so no layout change is needed.

## Risks / Trade-offs

- [Cost Explorer lags by hours] → It is still far ahead of a zeroed budget, and the watch keeps checking for 48 hours. The Budgets action remains the backstop after that.
- [The guard can now attach the freeze policy, not just detach it] → Listed under Security-relevant. It is limited to the two freeze policies, and only for a scope in `enforce` whose Cost Explorer spend is over its threshold. That is what the Budgets action already does, through a role the guard can't assume.
- [Cost Explorer and Budgets disagree slightly] → For a freeze, any overage within the watch window freezes. For a lift, a figure a little below Budgets' could lift slightly early. Both use unblended cost with the same cost types, so the difference is timing, not definition.
- [A Cost Explorer outage blocks lifts] → That is the safe direction, and the alert names the cause. Approvers can also remove the scope, which lifts without a spend check.
- [Cost] → At 0.01 USD per call, bounded as above.
