# Tasks

## 1. Builder

- [ ] 1.1 Add the Price List rows of the 4 models (and of Voxtral Mini, to show it is unchanged) from the 2026-11-16 Price List to `internal/prices/testdata/sample.json`, and the 4 models to the sample's Bedrock model list
- [ ] 1.2 Read direction from `inferenceType` in any wording, else from the usage type (design D1); test the Sonic and Grok rows
- [ ] 1.3 Drop `-mantle` and modality words from usage-type model parts, and key id-like `model` attributes with `idKey` (design D2); test Voxtral Small priced from its mantle rows and Voxtral Mini unchanged
- [ ] 1.4 Add the unique word-order fallback key (design D3); tests: Nova 2 Sonic matched, and an ambiguous bag key matching nothing
- [ ] 1.5 Add the no-lowering test (design D5): every model in the shipped table is priced the same or higher by the new build of the sample; and Rerank 3.5 and Pegasus still unpriced

## 2. Table and docs

- [ ] 2.1 Regenerate `internal/packs/ai/prices.json` with `guardrail prices build`, review the diff (the 4 models added, any raises explained), and confirm the shipped-table test passes
- [ ] 2.2 Add a CHANGELOG entry under Fixed, with the Upgrade note that the next shared-prices publish adds the 4 models; follow the docs' style (no em-dashes, Oxford comma)
- [ ] 2.3 Run `goimports`, `make`, `go vet ./...`, `go test ./...`, and cfn-lint; verify all pass

## 3. Live check

- [ ] 3.1 Run `guardrail prices publish --dry-run` against the test enterprise and confirm the 4 models are added at the Price List's rates and are gone from "no price found", leaving Kimi K3.1, GLM 5.3, Rerank 3.5, and both Pegasus models
