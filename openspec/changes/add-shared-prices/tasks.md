# Tasks

## 1. Facts

- [x] 1.1 Confirm the largest JSON cell Jennah accepts on insert and returns on read, and its gRPC message limit, with a test dataset; record them in `docs/aws-facts.md` (or a note in the design) and set the chunk cap (design D1) below both

## 2. Layout and Jennah client

- [x] 2.1 Declare `guardrail.prices` (`price_versions`, `price_chunks`) in `schema`, add it to the reader's reach in `GuardCredentials`, have `setup` create it once per enterprise, and reserve `prices` as an MSP id in `ValidateMSPID` (design D1, D5a, D6); verify schema and CLI tests for a first setup, a second payer org (prices left unchanged), the printed reach, and `--msp prices` refused by `setup` and `install-link`
- [x] 2.2 In `internal/jennah`, read the latest price manifest and a chunk by key, and publish a version (manifest plus chunks, immutable, chunk cap enforced); verify tests against the fake for read, publish, an oversized chunk, and a second publish of the same content writing nothing

## 3. Builder and publish command

- [x] 3.1 Move the price builder into `internal/prices` (Price List API joined with the Bedrock model list on several normalized keys, standard regional on-demand rates, unmatched models reported) (design D5); verify tests with recorded Price List and model list samples, including the naming mismatches seen (Instruct, IT, version suffixes, usage-type ids)
- [x] 3.2 Add `guardrail prices build -o FILE` (regenerates the embedded table) and `guardrail prices publish [--dry-run]` (prints the enterprise it acts on, shows added, raised, lowered, and removed models, then publishes) (design D5a); verify a CLI test for the difference output and a dry run writing nothing

## 4. Guard

- [x] 4.1 Pull prices on the 5-minute pull: read the manifest, fetch needed chunks whose hash changed, verify hashes, and keep chunks in state; fall back to the last good copy or the release table and alert once on each change in state (design D2, D4); verify tests for no change (no chunk fetched), a new version, a corrupt chunk, Jennah down, and a key without reach
- [x] 4.2 Merge in the AI pack: higher of release and shared price per input and output, provider floor for shared-only models, per-org `ai.prices` winning (design D3); verify tests for a higher shared price, a lower one (ignored), a new model floored at its provider's maximum, a provider absent from the release, and an override
- [x] 4.3 Write `prices_applied` with per-chunk raised, added, and ignored counts, and put the price version in cutoff and would-cutoff events; verify tests

## 5. Docs

- [x] 5.1 Update `docs/ai-guard.md` (where prices come from, tighten-only, what a price cut means), `docs/operating.md` (reader key reach including `guardrail.prices`, publishing prices once per enterprise, `prices` reserved as an MSP id), and add a `CHANGELOG.md` entry with Security-relevant noting the reader's added dataset and that shared prices can only tighten; follow the docs' style (no em-dashes, Oxford comma)

## 6. Build and verify

- [x] 6.1 Run `goimports`, `make`, and `go test ./...`; verify all pass
- [ ] 6.2 In the test enterprise: run `setup` (prices created), publish a version with one raised price, and with the e2e guard, confirm it fetches the chunk once, records `prices_applied`, and prices with the raised value; then publish a lower price and confirm it's ignored
