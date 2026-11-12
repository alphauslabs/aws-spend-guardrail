# Design

## Context

- `Reconcile` (`internal/guard/reconcile.go`) reads owned budgets (`ListOwned`), then calls `Budgets.Put` for each that differs from config, then handles removed scopes, `liftRaised`, and `watchSpend`.
- `Put` calls `UpdateBudget` on a limit, threshold, or filter change. In e2e, an `UpdateBudget` on a budget whose SCP action was in `EXECUTION_SUCCESS` made AWS reset the action within a second (`RESET_IN_PROGRESS`, then `STANDBY`). The reset reverses the action, so AWS's action worker detaches the policy. With the action in `STANDBY`, the same call changed nothing.
- `Release` reverses an executed action, or else detaches the policy directly. After a reset the action is in `STANDBY`, so it detaches, and `DetachPolicy` fails with `PolicyNotAttachedException` when the reset already did it.
- `Budgets.Freeze` (from `fix-budget-spend-reset`) attaches a freeze policy and treats an existing attachment as success.

## Goals / Non-Goals

**Goals:**
- A frozen scope stays frozen across any guard update of its budget, unless the update lifts it.
- The record of that freeze stays one freeze: no new `freeze` event or alert.

**Non-Goals:**
- Re-attaching a policy a person detached. That stays drift, reported and not undone.
- Avoiding the reset. AWS does it on any budget update, and the update is needed to apply the config.

## Decisions

### D1. Know which updates will reset
`BudgetStatus` gains `ActionExecuted bool`, set by `ListOwned` from the SCP action's status (`EXECUTION_SUCCESS`). Reconcile notes each budget it updates whose status read `ActionExecuted` before the update and whose target is recorded as frozen. Only those can be reset by AWS. A freeze the guard attached itself (action in standby) isn't touched by the update.

### D2. Carry the freeze after the reset, in the same run
After `liftRaised` (so a raise that lifts is handled first), for each noted budget whose target is still recorded frozen:
1. Poll `Budgets.ActionStatus` (new, a `DescribeBudgetActionsForBudget` read) every 2 seconds, for up to 30 seconds, until it reads `STANDBY`. The reset starts asynchronously, so a first read of `EXECUTION_SUCCESS` means "not started yet", not "done".
2. Once it's in standby, call `Budgets.Freeze` and record `freeze_carried` (scope, config version, `frozen_since`).
3. If the 30 seconds pass first, store a `carry_freeze` reported record for the budget. Each later run checks it first: once the action is in standby, it attaches and clears the record. A record for a target no longer recorded frozen is cleared.

The 30-second cap keeps the 5-minute run well inside its timeout. In e2e the reset finished within a second.

*Alternative:* re-attach any recorded freeze whose policy is missing, on every run. Rejected, because it would also undo a person's deliberate detach, which design keeps as drift.

### D3. A lift tolerates the reset's detach
`Release`, when it falls back to `DetachPolicy`, treats `PolicyNotAttachedException` as success and reports `how: already_detached`. The lift event then records that the policy was already gone.

### D4. Ordering with the spend watch
`watchSpend` runs after the carry step and skips frozen scopes as now, so a carried freeze isn't attached twice.

## Risks / Trade-offs

- [A reset slower than 30 seconds leaves the scope unfrozen until the next run, about 5 minutes] → Bounded, and recorded so it isn't lost. It's far better than until the next period.
- [The poll adds up to 30 seconds to a run that updates a frozen budget] → Rare (config changes to frozen scopes), and bounded.
- [AWS changes reset behavior] → If no reset happens, the action stays in `EXECUTION_SUCCESS` and the poll times out. The next run sees the policy still attached, and `Freeze` is a no-op on an existing attachment. The carry record is cleared once the action is in standby or the scope is no longer frozen.
