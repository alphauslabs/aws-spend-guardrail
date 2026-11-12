# Tasks

## 1. Check first

- [x] 1.1 Add the wording check to the `alerts` fake in `internal/guard` and to the engine's test alerter in `internal/activity` (design D3); confirm the current suites fail on the alerts listed in the proposal

## 2. Helpers and wording

- [x] 2.1 Add `targetName`, `targetRef`, and `liftReason` in `internal/guard/alerts.go`, and `WithError` with the wording check in `internal/alerttext` (design D1), with unit tests
- [x] 2.2 Reword in `internal/guard`: spend frozen and both drift alerts (`freeze.go`), freeze lifted and reconcile incomplete (`reconcile.go`), config not refreshed, still not refreshed, recovered, and version rejected (`pull.go`), event not queued and code hash unknown (`guard.go`), shared prices not read (`prices.go`), and move the Cost Explorer error in `spend.go` to an `Error:` line
- [x] 2.3 Reword in `internal/activity/engine.go` (could not cut off) and `guard/main.go` (AI guard not started)
- [x] 2.4 Add a test for the alerts the suites don't otherwise reach (code hash unknown, AI guard not started); update tests that match old text; confirm every suite passes the check

## 3. Docs and build

- [x] 3.1 `CHANGELOG.md` Changed: alert bodies are full sentences with error detail on a final `Error:` line; subjects unchanged; no em-dashes, Oxford comma
- [x] 3.2 Check docs quoting alert text (`docs/*.md`) and update any that changed
- [x] 3.3 Run `goimports`, `make`, and `go test ./...`; verify all pass

## 4. Live check

- [ ] 4.1 With e2e on this build, confirm the next freeze or lift email in the e2e tests reads as specified
