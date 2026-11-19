## 1. Known price

- [ ] 1.1 `ai.Pack.KnownPrice(key)`: the lowest input and output across regions the release or shared tables list the model in, merged tighten-only without the floor or config; false when no table lists it
- [ ] 1.2 Tests: release only, shared only (no floor applied), both (tighten-only), several regions (lowest per side), unlisted model

## 2. Daily check

- [ ] 2.1 Add `schema.KindPriceOverrideLow = "price_override_low"`
- [ ] 2.2 `Guard.checkPriceOverrides` in the daily run, when the AI guard is on and a config is in force: build the pack as `fast.go` does, compare each `ai.prices` entry with `KnownPrice`, and for each low entry not yet reported, alert and emit the event (id `price_override_low:<key>`), then mark it reported
- [ ] 2.3 Clear `price_override_low` reported records whose key isn't low today
- [ ] 2.4 Alert text per design D4, checked by `alerttext.Check`

## 3. Tests

- [ ] 3.1 A shared price rises above an entry in every region: one alert and one event with both prices; pricing still uses the entry
- [ ] 3.2 Low only in some regions, or low only against the floor, or a model no table lists: nothing reported
- [ ] 3.3 The next day with no change: nothing; the known price rises again: reported again with the new price
- [ ] 3.4 The entry raised or removed: the record is cleared, no alert; low again later: reported again
- [ ] 3.5 AI guard off: no check
- [ ] 3.6 `go vet ./...` and `go test ./...` pass

## 4. Docs, console, and release

- [ ] 4.1 `docs/operating.md` manual prices step 5 and `docs/ai-guard.md` price section: the guard reports a config price below the known price
- [ ] 4.2 CHANGELOG `[Unreleased]`: the new check and event kind, no template change, rollback supported
- [ ] 4.3 Jennah console (`app/src/lib/guardrail.ts`): label and summary for `price_override_low`, released as a console patch
- [ ] 4.4 Release as a patch and deploy to e2e; check the daily run with a test `ai.prices` entry below a known price, then remove it
