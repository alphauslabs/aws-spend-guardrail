# Proposal

## Why

A scope set to `month_end: stay_frozen` (the default) must stay frozen into a new budget period until a raise is approved. We don't yet know what AWS Budgets does to an executed freeze action when a new period starts (docs/aws-facts.md, "to confirm" until checked after 2026-12-01). If AWS resets it the way it resets one when a budget is updated, the reset detaches the freeze policy, and today:

- the scope can spend again, silently;
- the daily drift check (00:15 UTC) then finds the policy detached, treats it as detached by a person, alerts, and **clears the guard's freeze record**, so nothing would ever re-attach it.

The guard already handles the same AWS behavior after a budget update (fix-action-reset-unfreeze: "carry" re-attaches the policy). Handling the period case the same way makes the guard correct whichever way AWS behaves, so the December observation becomes a confirmation, and add-budget-freeze-guard 1.1 and the v0.1.0 release need not wait for it.

## What Changes

- On every pull and before the daily drift check, the guard looks at each scope it recorded as frozen whose config is `stay_frozen` and whose budget period has started since the freeze. If its freeze policy is no longer attached and the budget's action reads as reset by AWS (`STANDBY`), the guard attaches the policy again and writes a `freeze_carried` event saying AWS reset the action at a new period. If the reset is still in progress, it finishes on a later run, as a budget-update carry does.
- The drift check leaves alone a freeze whose action AWS reset at a new period or whose carry is still pending; a policy detached while its action still reads as executed is still treated as detached by a person, alerted, and cleared, as today.
- `auto_lift` scopes are unchanged: the guard lifts them at a new period itself.

## Capabilities

### New Capabilities

### Modified Capabilities

- `spend-freeze`: a `stay_frozen` freeze is kept across a new budget period even when AWS resets its action.

## Impact

- `internal/guard/carry.go`: a period-reset check feeding the existing carry; `internal/guard/freeze.go`: called in `Daily` before `Drift`, and `Drift` skips AWS resets and pending carries; `internal/guard/reconcile.go`: called on every pull.
- One action-status read per frozen `stay_frozen` scope whose policy is found detached in a new period; the policy attachment lists are already read by the drift check.
- Docs: `docs/operating.md` (month end), `docs/aws-facts.md` (the open row now says the guard handles both outcomes), CHANGELOG.
