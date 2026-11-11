# Tasks

## 1. Event ids

- [x] 1.1 In `internal/guard/freeze.go`, build the freeze id as `freeze:<target>:<period start>:<Since unix>` from the recorded freeze (design D1); keep `would_freeze` per period
- [x] 1.2 In `OnThreshold`, when the scope is already frozen, re-emit the freeze event from the stored record (same id) without alerting, instead of returning with nothing written (design D2)
- [x] 1.3 Tests: frozen, lifted, and frozen again in one month gives two `freeze` events and one `lift`; a Budgets notification after a guard attach adds no event and no alert; a repeated notification with no event recorded yet writes it once with the original time; `would_freeze` stays one per period

## 2. Docs and build

- [x] 2.1 `CHANGELOG.md` Fixed: a second freeze of a scope in the same budget period was dropped from the inbox; no em-dashes, Oxford comma
- [x] 2.2 Run `goimports`, `make`, and `go test ./...`; verify all pass

## 3. Live check

- [ ] 3.1 With e2e on this build, after the next freeze of e2estandard03 in November, confirm the inbox shows a new `freeze` event with the new id format after the 21:15 lift
