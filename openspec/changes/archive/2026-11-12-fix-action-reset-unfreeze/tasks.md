# Tasks

## 1. AWS adapter

- [x] 1.1 Add `ActionExecuted` to `BudgetStatus`, set by `ListOwned` from the SCP action's status, and `ActionStatus(ctx, name)` to the `Budgets` interface and `internal/awsx` (design D1, D2); test both
- [x] 1.2 In `Release`, treat `PolicyNotAttachedException` from the fallback `DetachPolicy` as success with `how: already_detached` (design D3); test it

## 2. Carry the freeze

- [x] 2.1 Add the `freeze_carried` event kind in `schema`, with docs in the inbox event list
- [x] 2.2 In reconcile, note updated budgets that had `ActionExecuted` and a recorded freeze; after `liftRaised`, wait for standby (2-second polls, 30 seconds at most), attach with `Freeze`, and record `freeze_carried`; on timeout store a `carry_freeze` record that later runs complete or clear (design D2)
- [x] 2.3 Make the fake Budgets API in `internal/guard` simulate the reset: an update of a budget whose action has executed detaches its policy and returns the action to standby after a given number of status reads
- [x] 2.4 Tests: a tightening of an action-frozen scope ends attached, with one `freeze` event, one alert, and one `freeze_carried`; a raise still below spend ends attached; a raise above spend lifts with `already_detached` and doesn't re-attach; a reset slower than the wait is carried on the next run; a guard-attached freeze (action in standby) has no wait and no `freeze_carried`

## 3. Docs and build

- [x] 3.1 `docs/aws-facts.md`: updating a budget whose action has executed resets the action and detaches its policy within a second (confirmed in e2e, with the CloudTrail and action history evidence); `docs/operating.md`: a budget change keeps a freeze, and `freeze_carried`
- [x] 3.2 `CHANGELOG.md` Fixed: a config change to a scope frozen by its Budgets action unfroze it; no em-dashes, Oxford comma
- [x] 3.3 Run `goimports`, `make`, and `go test ./...`; verify all pass

## 4. Live check

- [x] 4.1 With e2e on this build and e2estandard03 frozen by its Budgets action, publish a tightening, and confirm the policy is attached after the run, CloudTrail shows AWS's detach followed by the guard's attach, and the inbox has `freeze_carried` and no new `freeze`
