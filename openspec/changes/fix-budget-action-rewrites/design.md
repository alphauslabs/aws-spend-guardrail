# Design

## Context

`Budgets.Put` (`internal/awsx/budgets.go`) already compares the budget (`sameBudget`) and the notification (`syncNotification`, with `isPercentage` because Budgets omits a percentage threshold type) before writing. For an enforcing scope it then always calls `UpdateBudgetAction` with the full wanted action. `DescribeBudgetActionsForBudget` returns the current action, which `Put` already reads (`b.action`) to decide between create, update, and delete.

## Goals / Non-Goals

**Goals:**
- No Budgets write for an unchanged scope.
- Drift in any field the guard sets is still put back on the next run.

**Non-Goals:**
- Learning whether `UpdateBudgetAction` resets the calculated spend. Not writing makes the answer irrelevant.
- Changing create, delete, or release behavior.

## Decisions

**D1. Compare the fields the guard sets, normalizing what Budgets returns.** `sameAction(cur btypes.Action, want)` compares: `ActionThreshold.ActionThresholdValue`, `ActionThresholdType` (empty counts as percentage, as for notifications), `NotificationType`, `Definition.ScpActionDefinition.PolicyId`, its `TargetIds` as a set, `ExecutionRoleArn`, `ApprovalModel`, and `Subscribers` as a set of type and address. Fields the guard doesn't set (status, action id, budget name) are ignored. Alternative: hash the update input and keep it in state; rejected because it would miss drift made outside the guard.

**D2. An action that has run is never rewritten just because it has run.** Status isn't compared, so a frozen scope (`EXECUTION_SUCCESS`) or one waiting on a reset is left as it is unless config changed a field. Release keeps its own path.

## Risks / Trade-offs

- **Budgets returns another field in a different form than it accepts** (like the threshold type), making every comparison differ and the guard write every run, as today. Mitigation: the e2e check (task 3.1) looks for `UpdateBudgetAction` in the payer's CloudTrail event history after a few runs; there should be none.
- **A field Budgets adds later and the guard doesn't set** is ignored, so it isn't put back. Acceptable: the guard only owns what it sets.
