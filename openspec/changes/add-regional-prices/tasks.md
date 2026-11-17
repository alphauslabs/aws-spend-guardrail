# Tasks

## 1. Facts first

- [ ] 1.1 From the e2e payer: time a full per-region Price List read and the model lists of every enabled region (Bedrock and mantle) with up to 8 regions at a time; confirm the IAM action the mantle `GET /v1/models` needs; record both in the design (D5, D6) and adjust the concurrency or timeout if the build doesn't finish well inside 300 seconds

## 2. Builder

- [ ] 2.1 `RegionalTable` (design D1); `Fetch` reads the Price List per region and the model lists of each enabled region, concurrently, naming regions it couldn't list (D5)
- [ ] 2.2 `Build` prices each listed model per region with the existing matching, plus mantle ids, dated snapshots, and the `moonshotai.` alias (D5 step 4), and prices models in unlisted regions from those regions' entries (step 5)
- [ ] 2.3 Tests: the recorded Price List sample extended with two more regions' entries prices every model the same or higher in us-east-1 as before (the builder-never-lowers rule); a regional-only model, a mantle-only model, a dated snapshot, and the alias are priced; a region whose list fails is named
- [ ] 2.4 Regenerate `internal/packs/ai/prices.json` in the regional format, and `ai.Prices()` reads it; `TestPriceTableIsSane` checks every region

## 3. Publishing

- [ ] 3.1 `Publish` writes one chunk per region, the `us-east-1` chunk with the extra models at their highest regional price and the `manual` field (D2); the no-change check compares all chunks
- [ ] 3.2 Manual prices apply in every region; auto-drop and the stale-manual alert follow D7
- [ ] 3.3 `guardrail prices build` and `publish` output and the scheduled publish's change alert summarize per model across regions (D8); the first regional version's note says so
- [ ] 3.4 Tests: a v0.2.4-style reader (only `ai/us-east-1`) prices a regional-only model from the extra entry; manual price kept while one offered region lacks a covering price; dropped once all do

## 4. Guards

- [ ] 4.1 Pull every `ai/<region>` chunk whose hash changed into meta `prices.ai.<region>`, drop regions no longer listed, and alert once per corrupt chunk (D3)
- [ ] 4.2 `ai.Pack.Price(model, region)` per D4, used for trail records' `awsRegion`; the unknown price is the highest across all regions; the shared-only floor per region
- [ ] 4.3 Tests: a call in eu-west-2 at the eu-west-2 price; a model with no price in the call's region at its highest; an override in every region; the floor for a shared-only model; state fixture for this release includes `prices.ai.eu-west-2`, and the previous release's fixture test reads it (rollback)

## 5. Stack and docs

- [ ] 5.1 Template: timeout 300, `ec2:DescribeRegions` and the mantle model-list action in the price-publishing policy, `TEMPLATE_REVISION` and `MinTemplateRevision` 4; tests and cfn-lint pass
- [ ] 5.2 Docs: `ai-guard.md` (prices per region, the fallback, global profiles priced regionally), `operating.md` (manual prices in every region, build and publish output), `aws-facts.md` (the facts in the design's Context), `upgrade.md`; CHANGELOG with Upgrade (update the stack, revision 4; earlier guards keep us-east-1 prices until upgraded), Security-relevant (the new read permissions), and rollback; docs style (no em-dashes, Oxford comma)
- [ ] 5.3 Update add-mantle-usage: its D4 keeps only the version-suffix match the builder doesn't resolve, mantle usage is priced at its region's price, and its template revision becomes 5
- [ ] 5.4 Run `goimports`, `make`, `go vet ./...`, `go test ./...`, and cfn-lint; verify all pass

## 6. Live check

- [ ] 6.1 Deploy to e2e with publishing on; run the scheduled publish; confirm a version with a chunk per region, the `us-east-1` chunk unchanged for existing models and with the regional-only models added, and the run inside the timeout
- [ ] 6.2 With the AI guard on (dry-run scope for e2enormal), make a `Converse` call in ap-northeast-1 from e2enormal and confirm it's counted at the ap-northeast-1 price; then deploy v0.2.4's package and confirm it reads the new version and prices the same call at the us-east-1 price (rollback)
- [ ] 6.3 Remove the test setup: the AI guard off, the stack back on its release, and any temporary trail and bucket
