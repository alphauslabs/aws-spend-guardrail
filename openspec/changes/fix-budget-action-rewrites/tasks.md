# Tasks

## 1. Compare before updating

- [x] 1.1 Add `sameAction` in `internal/awsx/budgets.go` comparing threshold value and type (empty type as percentage), notification type, policy, target ids as a set, execution role, approval model, and subscribers as a set (design D1, D2); in `Put`, call `UpdateBudgetAction` only when it reports a difference
- [x] 1.2 Have the fake Budgets API count action updates and return the threshold type empty, as Budgets does; extend `TestUnchangedBudgetWritesNothing` to require no action update for an enforcing scope over repeated runs, and add a test that a changed threshold, role, or subscriber updates the action exactly once and a frozen action with unchanged config is not updated

## 2. Docs and build

- [x] 2.1 Add a `CHANGELOG.md` entry under Fixed (the guard rewrote an enforcing scope's freeze action whenever any part of the scope changed), with Security-relevant: None for this change; no em-dashes, Oxford comma
- [x] 2.2 Run `goimports`, `make`, and `go test ./...`; verify all pass

## 3. Live check

- [ ] 3.1 With the e2e guard on this build, change only one scope's limit in config, and after at least three runs confirm `aws cloudtrail lookup-events` in the payer account shows an `UpdateBudget` but no `UpdateBudgetAction` since the deploy, and that the e2estandard03 action still has its threshold, policy, and target
