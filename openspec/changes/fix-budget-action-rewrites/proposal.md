# Proposal

## Why

On every run (every 5 minutes, and on budget and account events), the guard calls `UpdateBudgetAction` on each enforcing scope's freeze action, even when nothing about the action changed. The budget and its notification are already left alone when unchanged, because replacing a notification was seen to reset the budget's calculated spend to zero (2026-11-10), which kept spend from ever reaching the threshold. Whether rewriting an action has a similar effect, on the calculated spend or on an action that is pending or has run, isn't documented, and it can't be ruled out while AWS billing data is slow to refresh. Writing only on a real change removes the doubt, and it also cuts needless Budgets API calls and CloudTrail noise in the payer account.

## What Changes

- **The guard updates a freeze action only when it differs** from what config wants: its threshold (value and type, counting a missing type as a percentage, as Budgets returns it), notification type, the policy and target it attaches, its execution role, its approval model, or its subscribers. Otherwise it writes nothing.
- **A test records that an unchanged enforcing budget makes no write at all** (budget, notification, or action) across repeated runs, and that each of those fields, when changed, causes exactly one update.

Non-goals: changing when actions are created or deleted (switching between dry-run and enforce stays as is), and how a freeze is released.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `spend-freeze`: states that reconciling an unchanged scope writes nothing to Budgets. The reconcile requirement was introduced by `add-budget-freeze-guard`, not archived yet, so the delta adds a requirement.

## Impact

- **Code:** `internal/awsx/budgets.go` (`Put`, a new action comparison) and `internal/awsx/budgets_test.go` (the fake records action updates).
- **Deployed guards:** after an upgrade, a guard stops calling `UpdateBudgetAction` on unchanged scopes. No stack parameter, IAM permission, or policy changes.
- **Docs:** `CHANGELOG.md` (Fixed; Security-relevant: None).
