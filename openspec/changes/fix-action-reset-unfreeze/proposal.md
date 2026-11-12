## Why

When the guard updates a budget whose freeze action has executed, AWS Budgets resets the action, and the reset reverses it: the freeze policy is detached about a second later. The scope is unfrozen while the guard, the inbox, and the alert all say it's frozen. Nothing freezes it again until the next budget period. The Budgets action is back in standby against a budget that now reports zero spend, and the guard's spend watch skips the scope because it's recorded as frozen. The daily drift check reports it, but doesn't re-attach.

The e2e live test showed this on 2026-11-12. e2estandard03 was frozen by the Budgets action at 02:47. At 05:40:15, config version 6 (cap 5.50 to 5.40 USD, a tightening) made the guard call `UpdateBudget`. The guard's spend watch attached the policy at 05:40:15, and AWS Budgets (`InsightsActionWorker`, through the stack's execution role) detached it at 05:40:16. The action history shows `RESET_IN_PROGRESS` at 05:40:15.893. The same `UpdateBudget` at 22:25 the night before caused no reset, because that freeze had been attached by the guard and the action was in standby.

So any config change to a scope frozen by the Budgets action unfreezes it. That includes a tightening, a raise that's still below spend, and a threshold or mode change.

## What Changes

- Budget status as the guard reads it includes the freeze action's state.
- When the guard updates the budget of a scope it has recorded as frozen, and the budget's action had executed, it waits for AWS's reset to finish (the action back in standby, up to 30 seconds). Then it attaches the freeze policy itself, unless this run lifts the freeze. If the reset hasn't finished in time, the next run completes it.
- That freeze is carried over, not new. The freeze record and its `Since` are kept, so no second `freeze` event is written and no second alert is sent. A `freeze_carried` event records that the guard re-attached the policy after a budget change, with the config version.
- A lift of a scope whose policy AWS's reset already detached succeeds. Detaching a policy that's no longer attached counts as done.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `spend-freeze`: a budget change keeps a freeze in force.

## Impact

- `internal/guard` (reconcile, a new `freeze_carried` event kind in `schema`) and `internal/awsx/budgets.go` (action status in `BudgetStatus`, an `ActionStatus` read, and `Release` tolerating a policy that's already detached).
- Tests in both packages, with the fake Budgets API simulating the reset.
- `docs/aws-facts.md` (the reset, confirmed in e2e), `docs/operating.md`, and `CHANGELOG.md` (Fixed). No IAM change: the guard can already attach the freeze policies.
