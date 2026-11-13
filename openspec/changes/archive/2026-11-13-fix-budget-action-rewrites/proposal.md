# Proposal

## Why

The guard's reconcile calls `Budgets.Put` for a scope only when the scope's spec (limit, period, threshold, mode, policy, or target) differs from the budget it finds, so a fully unchanged scope already makes no Budgets write; the payer's CloudTrail event history in the e2e org shows no `UpdateBudgetAction` call in 90 days. But once `Put` runs, it calls `UpdateBudgetAction` on an enforcing scope's freeze action unconditionally, so a change to anything else about the scope (for example, only its limit) also rewrites an action whose fields are all unchanged. The budget and its notification are already left alone when unchanged, because replacing a notification was seen to reset the budget's calculated spend to zero (2026-11-10), which kept spend from ever reaching the threshold. Whether rewriting an action has a similar effect, on the calculated spend or on an action that is pending or has run, isn't documented. Writing the action only when it differs removes the doubt.

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
- **Deployed guards:** after an upgrade, a guard no longer rewrites an unchanged freeze action when some other part of the scope changes. No stack parameter, IAM permission, or policy changes.
- **Docs:** `CHANGELOG.md` (Fixed; Security-relevant: None).
