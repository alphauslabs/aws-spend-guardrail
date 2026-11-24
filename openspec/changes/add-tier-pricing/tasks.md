# Tasks

## 1. Facts

- [ ] 1.1 From the e2e payer, with the AI guard off, make one call each on `openai.gpt-oss-20b-1:0` with the Priority tier: `InvokeModel` (tier in the `X-Amzn-Bedrock-Service-Tier` header), `ConverseStream`, and `InvokeModelWithResponseStream`, with at most 5 output tokens; read the records with `aws cloudtrail lookup-events`, and note which carry the tier and where (design D3)
- [ ] 1.2 Find how an Ultrafast call is requested (AWS docs, the CLI's `--service-tier` values) and, if a model in the e2e regions offers it, make one call and read its record; if Ultrafast is requested another way, note the field (design Open Questions)
- [ ] 1.3 Record the findings in `docs/aws-facts.md` with the date: which record kinds carry the tier, the field, and that a call without a tier has none; check `go test ./tools/...` and the docs build still pass

## 2. Builder

- [ ] 2.1 In `internal/prices/build.go`, take `flex`, `priority`, and `ultrafast` out of `skipUsage` and parse them as tiers from the usage type or `inferenceType`, keeping rows with any other skip word dropped (design D1); test with the GPT-6 Astra standard, Ultrafast, and long-context Ultrafast rows that the standard price is $0.011 and $0.055 and the Ultrafast tier $0.066 and $0.33
- [ ] 2.2 Add `Tiers` to the built table and per-region chunks, and `region_tiers` to the release table type (design D2); test that a chunk round-trips through JSON with its tiers, and that a chunk with tiers parses into the v0.2.17 table type unchanged
- [ ] 2.3 Regenerate `internal/packs/ai/prices.json` with `guardrail prices build`; check with `TestBuildNeverLowersAShippedPrice` and `TestBuildMatchesTheShippedTable` that no standard price changed

## 3. Publishing

- [ ] 3.1 Diff, merge, and report tier prices per model, tier, and region, naming the tier in each change line, with the 2x note per tier (design D6); test added, raised, and lowered tier prices, and that an unchanged tier writes no version
- [ ] 3.2 Make the first publish with tiers note that it adds tier prices; test its note and that manual prices carry forward unchanged

## 4. Guard

- [ ] 4.1 Parse `requestParameters.serviceTier.type` into `activity.Record.ServiceTier`, and any other field task 1 finds; add the task 1 records, scrubbed of real ids, to `internal/activity/testdata` and test the parsing
- [ ] 4.2 Load tier prices from the release and shared chunks into the AI pack, merged tighten-only per tier (design D2, D4); test that a shared tier price raises a release tier price and never lowers it
- [ ] 4.3 Price calls by tier: standard, a named tier, and can't-tell or unpriced tier at the model's highest, with `ai.prices` as the standard price and a floor for other tiers, and tier prices in the unpriced-model price (design D3, D4); test each scenario in the ai-pack delta
- [ ] 4.4 Mark usage from metrics and the record kinds task 1 found tier-blind; test that mantle usage of a model with an Ultrafast tier is priced at it, and a model without tiers at its standard price
- [ ] 4.5 Report an unpriced tier once per model and tier with an alert and an `unpriced_tier` event (design D5), and add the event kind to `schema`; test one report for 200 calls

## 5. Docs and release

- [ ] 5.1 Update `docs/ai-guard.md` (pricing by tier, tier-blind usage, `ai.prices` and tiers) and `docs/operating.md` (tier lines in publish output, the first publish with tiers); check style: no em-dashes, Oxford comma
- [ ] 5.2 Add the CHANGELOG entry: package-only upgrade, the estimate changes (premium tiers and mantle up, Flex down), and Security-relevant: None; check with `go run ./tools/changelogcheck`
- [ ] 5.3 Run `go vet ./...` and `go test ./...`, and the release gates

## 6. Live check

- [ ] 6.1 Deploy to e2e with the AI guard on in dry-run for one test account; make one standard and one Priority `Converse` call to `openai.gpt-oss-20b-1:0`; check the account's AI totals grew by the standard and the Priority price for their tokens; turn the AI guard off again and remove any test setup
- [ ] 6.2 Publish shared prices with the new build; check the version lists tier prices as added, and that a guard of v0.2.17 still reads it (its heartbeat and logs show no price read errors)
