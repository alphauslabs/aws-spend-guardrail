# Tasks

## 0. Order

- [ ] 0.1 Archive add-shared-prices first (all its tasks are done), so the `shared-prices` capability exists in the main specs before this change's delta is applied

## 1. File and merge

- [ ] 1.1 In `internal/prices`, read and validate the manual price file: strict fields, normalized ids, both prices above zero, no duplicates after normalizing (design D1); tests for a valid file, an unknown field, a zero price, and `us.` plus bare id of one model
- [ ] 1.2 Merge manual prices into a built table: higher of built and manual per side for priced models, added otherwise, `basis` noting manual prices; record `manual` with `set_in_version` (design D2, D3); tests for a missed model, a manual price below the Price List (kept higher, reported), and above it

## 2. Publish

- [ ] 2.1 In `cli.PublishPrices`, start from the latest chunk's `manual`, apply the file and `--drop-override` (an unknown model is an error), require `--note` when manual prices change, and keep the same-content rule (design D3, D4, D6); tests: a routine publish carries manual prices forward with no note, a new manual price without a note is refused with nothing written, a drop removes the model unless the Price List prices it, identical inputs write nothing
- [ ] 2.2 Print manual marks, "Price List is higher", "the Price List now prices it", and the provider-floor warning in the diff output (design D5); a CLI test for each line
- [ ] 2.3 Add `--overrides FILE` and `--drop-override MODEL` (repeatable) to `guardrail prices publish` and its usage text

## 3. Docs and build

- [ ] 3.1 Document manual prices in `docs/operating.md` (Shared AI prices: the file format, carry-forward, dropping, the note rule, and the floor warning) and `docs/ai-guard.md` (Prices: where a manual price comes from); add a CHANGELOG entry; follow the docs' style (no em-dashes, Oxford comma)
- [ ] 3.2 Run `goimports`, `make`, `go vet ./...`, and `go test ./...`; verify all pass

## 4. Live check

- [ ] 4.1 In the test enterprise: publish with a file pricing one model the build missed and a note, confirm the version's chunk has the model and `manual`; publish again without the file and confirm it is carried forward with nothing new written; drop it with a note and confirm it is gone
