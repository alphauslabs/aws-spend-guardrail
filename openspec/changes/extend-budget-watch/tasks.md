# Tasks

## 1. Watch past 48 hours

- [ ] 1.1 In `internal/guard/watch.go`, replace the 48-hour end with D1: past `SpendWatchFor`, end the watch only when the hourly Cost Explorer read shows zero cost; continue on a cost above zero or a failed read. Verify with `watch_test.go` cases: a scope with no spend ends at 48 hours, a spending scope whose budget reads zero is still watched and frozen at its threshold at 50 hours, and a failed read past 48 hours keeps the watch.
- [ ] 1.2 Add the `spend_lag` record and the two alerts (D2): once when a spending scope's budget still reads zero past 48 hours, once when it catches up with the hours it took; clear the record on every end of the watch. Verify with tests: one lag alert over several hourly runs, one caught-up alert naming the hours, and no caught-up alert when the watch ends at a new period.
- [ ] 1.3 Update `docs/operating.md` (When Budgets reports no spend) and the Cost Explorer cost line in `docs/install.md`, and add a CHANGELOG entry; verify the docs name both alerts and the changelog passes `go run ./tools/changelogcheck CHANGELOG.md vX` at release.

## Workflow follow-up

- Release, deploy to e2e, and archive the change.
- When a lag alert is seen, record the lag in `docs/aws-facts.md`.
