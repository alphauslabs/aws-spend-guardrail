# Design

## Context

See proposal.md for why. `carryFreezes` (internal/guard/carry.go) re-attaches a freeze after AWS resets an executed action on a budget the guard updated: it waits for the action to read `STANDBY` (`resetDone`), calls `Budgets.Freeze`, emits `freeze_carried`, and keeps a `carry_freeze` reported marker for resets that outlast a run. `Daily` runs `periodLifts` (auto_lift), then `Drift`, then coverage and the heartbeat, then `Reconcile`. `Drift` treats any recorded freeze whose policy isn't attached as detached by a person: it alerts and calls `ClearFrozen`. `Frozen` records `PeriodStart`, the period the freeze fired in.

## Goals / Non-Goals

**Goals:** a `stay_frozen` freeze survives a new period whichever way AWS behaves; a person detaching a freeze is still reported as drift.

**Non-Goals:** changing `auto_lift` (the guard lifts those itself); detecting AWS's period behavior in advance (the December observation records it).

## Decisions

**D1. A period-reset check feeds the existing carry.** `carryPeriodResets(ctx, eff)` lists recorded freezes, keeps those whose scope is in the config in force with `month_end` not `auto_lift`, whose current period start (`PeriodStart(scope.Period, now)`) is after the record's `PeriodStart`, and whose target the freeze policies' attachment lists don't include. For each, it reads the action status: `STANDBY` means AWS reset it, so the guard attaches the policy (`Budgets.Freeze`) and emits `freeze_carried` with `reason: "period_reset"`; `RESET_IN_PROGRESS` marks the budget `carry_freeze`, so the existing pending path finishes it on a later run; an executed status means the policy was detached while the action stayed executed, which is left to `Drift`. Alternative: re-attach any detached `stay_frozen` freeze in a new period regardless of action status; rejected because it would also override a person who detached a freeze on purpose.

**D2. Where it runs.** In `Reconcile` (every pull), so a reset at any hour is undone within 5 minutes, and at the start of `Daily`, before `Drift`, so the 00:15 run on the first of a month carries first. The attachment lists are read once per call (two `ListTargetsForPolicy` calls); the action status is read only for a freeze found detached in a new period.

**D3. Drift skips what carry owns.** Before alerting and clearing, `Drift` skips a target whose budget is marked `carry_freeze`, and, for a `stay_frozen` freeze in a new period, a target whose action reads `STANDBY` or `RESET_IN_PROGRESS`. Only a detached policy with an executed action is drift.

**D4. The record keeps its original `Since` and `PeriodStart`.** The freeze is the same freeze; `freeze_carried` records the new period. Lifting on a later raise still compares against the record's threshold, as today.

## Risks / Trade-offs

- [A person detaches a freeze on purpose in a new period, and AWS has also reset the action.] → The action status can't tell them apart; the guard re-attaches, as `stay_frozen` asks. To end a freeze on purpose, approve a raise or set `auto_lift`; the docs say so.
- [AWS's period reset could differ from its update reset (for example, no `STANDBY` state).] → If the action reads as executed with the policy detached, it falls to drift as today, and the December observation will show it; the tests cover the documented update-reset states.

## Migration Plan

Ship in a guard release. No state changes. Rollback is the previous release.
