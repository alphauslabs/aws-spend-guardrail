# Tasks

## 1. Guard

- [x] 1.1 Add `carryPeriodResets` (design D1) feeding the existing carry, with `reason: "period_reset"` on its `freeze_carried` event
- [x] 1.2 Call it in `Reconcile` and at the start of `Daily`, before `Drift` (design D2)
- [x] 1.3 Make `Drift` skip pending carries and period resets of `stay_frozen` freezes, keeping drift for a detached policy whose action still reads as executed (design D3)
- [x] 1.4 Tests: a period reset re-attaches and records `freeze_carried` once; a reset in progress finishes on a later run; a detached policy with an executed action is still drift and isn't re-attached; `auto_lift` is untouched; a freeze in its own period with the policy detached is drift; nothing happens when the policy stays attached; the daily run carries before drift on the first of a month

## 2. Docs and build

- [x] 2.1 Document the month-end behavior in `docs/config.md` (where `month_end` is described) (a `stay_frozen` freeze stays even if AWS resets the action; ending one on purpose is a raise or `auto_lift`), update the open row in `docs/aws-facts.md`, and add a CHANGELOG entry
- [x] 2.2 Run `gofmt`, `go vet ./...`, and `go test ./...`; verify all pass

## 3. Live check

- [ ] 3.1 On e2e, deploy this build; after 2026-12-01, confirm e2estandard03 (`stay_frozen`, frozen since 2026-11-13) is still frozen, and whether a `freeze_carried` with `reason: period_reset` was recorded (it was, if AWS resets at a new period)

## Workflow follow-up

- Deploy to e2e before 2026-12-01; tick add-budget-freeze-guard 1.1 with the AWS fact left to the December observation; archive after 3.1.
