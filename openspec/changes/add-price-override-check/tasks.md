## 1. Known price

- [x] 1.1 `ai.Pack.KnownPrice(key)`: the lowest input and output across regions the release or shared tables list the model in, merged tighten-only without the floor or config; false when no table lists it
- [x] 1.2 Tests: release only, shared only (no floor applied), both (tighten-only), several regions (lowest per side), unlisted model

## 2. Daily check

- [x] 2.1 Add `schema.KindPriceOverrideLow = "price_override_low"`
- [x] 2.2 `Guard.checkPriceOverrides` in the daily run, when the AI guard is on and a config is in force: build the pack as `fast.go` does, compare each `ai.prices` entry with `KnownPrice`, and for each low entry not yet reported, alert and emit the event (id `price_override_low:<key>:<UTC day>`), then mark it reported
- [x] 2.3 Clear `price_override_low` reported records whose key isn't low today
- [x] 2.4 Alert text per design D4, checked by `alerttext.Check`

## 3. Tests

- [x] 3.1 A shared price rises above an entry in every region: one alert and one event with both prices; pricing still uses the entry
- [x] 3.2 Low only in some regions, or low only against the floor, or a model no table lists: nothing reported
- [x] 3.3 The next day with no change: nothing; the known price rises again: reported again with the new price
- [x] 3.4 The entry raised or removed: the record is cleared, no alert; low again later: reported again
- [x] 3.5 AI guard off: no check
- [x] 3.6 `go vet ./...` and `go test ./...` pass

## 4. Docs, console, and release

- [x] 4.1 `docs/operating.md` manual prices step 5 and `docs/ai-guard.md` price section: the guard reports a config price below the known price
- [x] 4.2 CHANGELOG `[Unreleased]`: the new check and event kind, no template change, rollback supported
- [ ] 4.3 Jennah console (`app/src/lib/guardrail.ts`): label and summary for `price_override_low`, released as a console patch
- [ ] 4.4 Release as a patch and deploy to e2e; check the daily run with a test `ai.prices` entry below a known price, then remove it
