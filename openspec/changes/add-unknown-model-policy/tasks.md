# Tasks

## 1. Config

- [x] 1.1 Add `unknown_models` to `AISection`, `ResolvedAI` (resolved to `cutoff` when absent), `config/schema.json`, and validation (only `cutoff` or `count`) (design D1); tests: absent resolves to `cutoff`, both values validate, `allow` fails naming the field and the accepted values
- [x] 1.2 Classify `ai.unknown_models` in `aiChanges` (`cutoff` to `count` loosening, reverse tightening) and word both in `Change.Sentence` (design D6); tests for both directions, the sentence, and the approval title "Loosen the AI guard"

## 2. AI pack

- [x] 2.1 Compute `unknownPrice` in `NewShared` from the release table, shared table, and `ai.prices`, input and output maxima separately (design D2); test with a shared-only model and an override above the release maximum
- [x] 2.2 In `Evaluate`, under `count`, price an allowed unpriced model at `unknownPrice` and set `Usage.Unpriced`; trip `unknown_model` when the record has no token counts; leave `cutoff` and the allowlist check as today (design D3, D4); tests: counted non-streaming call, counted streaming call (completion record), zero tokens trips, `cutoff` trips, not-allowlisted model trips `model_not_allowed`
- [x] 2.3 Add `Priced(model) bool` to the pack for the engine's pruning (design D5)

## 3. Engine and guard

- [x] 3.1 Add the `unpriced_model` kind to `schema`, and in the engine report each newly counted unpriced model once (event and alert) using the `unpriced.<pack>` state item saved with the reader commit, and drop models the pack now prices (design D5); tests: 500 calls from three accounts give one event and one alert, a failed usage write reports again on the next tick, a model priced then unpriced is reported again
- [x] 3.2 Test end to end through `Guard.Trail` with a harness config in `count` mode: an unpriced model's calls add to totals at the ceiling and an hourly cap cuts off with rule `hourly_cap`

## 4. Docs and build

- [x] 4.1 Document `ai.unknown_models` in `docs/ai-guard.md` (Prices and tripwires: what each value does, the ceiling and why it is high, the no-token-counts rule, the once-per-model alert and event) and in `docs/config.md` where the `ai` section's fields are described (corrected from `docs/operating.md` during apply); add a CHANGELOG entry; follow the docs' style (no em-dashes, Oxford comma)
- [x] 4.2 Run `goimports`, `make`, `go vet ./...`, and `go test ./...`; verify all pass

## 5. Live check

- [x] 5.1 On e2e with the AI guard on in dry-run: publish a version setting `unknown_models` to `count` (approve it), call a Bedrock model with no price (or temporarily remove one from shared prices), and confirm one `unpriced_model` event and alert, usage at the ceiling price, and no would-cutoff unless a cap is passed; then remove the test setup
