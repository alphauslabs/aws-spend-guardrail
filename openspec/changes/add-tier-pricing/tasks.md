# Tasks

## 1. Facts

- [x] 1.1 From the e2e payer, with the AI guard off, make one call each on `openai.gpt-oss-20b-1:0` with the Priority tier: `InvokeModel` (tier in the `X-Amzn-Bedrock-Service-Tier` header), `ConverseStream`, and `InvokeModelWithResponseStream`, with at most 5 output tokens; read the records with `aws cloudtrail lookup-events`, and note which carry the tier and where (design D3)
- [x] 1.2 Find how an Ultrafast call is requested (AWS docs, the CLI's `--service-tier` values) and try one call; record in `docs/aws-facts.md` how it is requested, and its record as "to confirm" while no e2e account can call a model that offers it (design D10, Open Questions)
- [x] 1.3 Record the findings in `docs/aws-facts.md` with the date: which record kinds carry the tier, the field, and that a call without a tier has none; check `go test ./tools/...` and the docs build still pass
- [x] 1.4 Check the AI cutoff refuses `ChatCompletions` and `Responses` on `bedrock-runtime`: with a test role in the e2e payer whose policy allows Bedrock but denies the cutoff's actions, make one Chat Completions call and expect `AccessDeniedException`; if it succeeds, find the action it needs and add it to the deny policy and `docs/ai-guard.md` (design D8); delete the role afterwards
- [x] 1.5 Make one streamed `ChatCompletions` call on `bedrock-runtime` and try a `Responses` call; note in `docs/aws-facts.md` which records carry the token counts and the tier, and a successful `Responses` record as "to confirm" if none can be made (design D8, D9)

## 2. Builder

- [x] 2.1 In `internal/prices/build.go`, take `flex`, `priority`, and `ultrafast` out of `skipUsage` and parse them as tiers from the usage type or `inferenceType`, keeping rows with any other skip word dropped (design D1); test with the GPT-6 Astra standard, Ultrafast, and long-context Ultrafast rows that the standard price is $0.011 and $0.055 and the Ultrafast tier $0.066 and $0.33
- [x] 2.2 Add `Tiers` to the built table and per-region chunks, and `region_tiers` to the release table type (design D2); test that a chunk round-trips through JSON with its tiers, and that a chunk with tiers parses into the v0.2.17 table type unchanged
- [x] 2.3 Regenerate `internal/packs/ai/prices.json` with `guardrail prices build`; check with `TestBuildNeverLowersAShippedPrice` and `TestBuildMatchesTheShippedTable` that no standard price changed

## 3. Publishing

- [x] 3.1 Diff, merge, and report tier prices per model, tier, and region, naming the tier in each change line, with the 2x note per tier (design D6); test added, raised, and lowered tier prices, and that an unchanged tier writes no version
- [x] 3.2 Make the first publish with tiers note that it adds tier prices; test its note and that manual prices carry forward unchanged

## 4. Guard

- [x] 4.1 Parse the tier from `requestParameters.serviceTier.type`, `additionalEventData.serviceTier`, and `additionalEntries.serviceTier` into `activity.Record.ServiceTier`, with `RequestID` and `ParentRequestID` (design D3); add the task 1 records, scrubbed of real ids, to `internal/activity/testdata` and test the parsing of each call kind
- [x] 4.2 Load tier prices from the release and shared chunks into the AI pack, merged tighten-only per tier (design D2, D4); test that a shared tier price raises a release tier price and never lowers it
- [x] 4.3 Price calls by tier: standard, a named tier, and can't-tell or unpriced tier at the model's highest, with `ai.prices` as the standard price and a floor for other tiers, leaving the unpriced-model price at the highest standard price (design D3, D4); test each scenario in the ai-pack delta
- [x] 4.4 Mark usage from metrics tier-blind; test that mantle usage of a model with an Ultrafast tier is priced at it, and a model without tiers at its standard price
- [x] 4.5 Give a `ConverseStream` completion its call record's tier by `parentRequestId`, kept 15 minutes, and price an unmatched one at the highest tier (design D7); test a matched Priority stream, a matched standard stream, and an unmatched one
- [x] 4.6 Price `ChatCompletions` and `Responses` calls like `InvokeModel`, streamed ones from the record task 1.5 found, under the same caps, allowlists, tripwires, and unknown-model policy (design D8); test a priced call, a Priority call, and a call to a model not on the allowlist
- [x] 4.7 Report an unpriced tier once per model and tier with an alert and an `unpriced_tier` event (design D5), and add the event kind to `schema`; test one report for 200 calls
- [x] 4.8 Add `ai.unmeasured_calls` (`cutoff` default, `allow`) to config with validation, classification (`cutoff` to `allow` loosening), and the change sentence, following `unknown_models` (design D9); test the default, a bad value, and both directions of the change
- [x] 4.9 Trip `unmeasured_call` on a successful record with no token counts (a streaming completion, or a non-streamed `ChatCompletions` or `Responses` call record), pricing `ChatCompletions` and `Responses` with `stream: true` from their completion, unless `ai.unmeasured_calls` is `allow`, then count it as one call at $0 and report the first per account and model with an alert and an `unmeasured_call` event; add the rule and event kind to `schema` (design D9); test with the streamed `ChatCompletions` records from task 1.5, scrubbed, in both settings, and that a direct `InvokeModelWithResponseStream` completion with tokens is priced as before
- [x] 4.10 Once a day, read each listed region's Ultrafast tokens per account and model from `AWS/Bedrock` with one search query, and report each account and model the trail showed no Ultrafast call for that day with one alert and an `ultrafast_unseen` event; keep the day's Ultrafast accounts and models seen in the trail; add the event kind to `schema` (design D10); test a report, no report when the trail showed the calls, and no read without listed regions

## 5. Docs and release

- [x] 5.1 Update `docs/ai-guard.md` (pricing by tier, tier-blind usage, `ai.prices` and tiers, OpenAI-compatible calls, unmeasured calls) and `docs/config.md` (`unmeasured_calls`) and `docs/operating.md` (tier lines in publish output, the first publish with tiers); check style: no em-dashes, Oxford comma
- [x] 5.2 Add the CHANGELOG entry: an Upgrade step to set and approve `ai.unmeasured_calls: allow` before updating the stack for orgs whose accounts stream Chat Completions, then the package-only update; that OpenAI-compatible runtime calls now count toward caps and streamed ones trip `unmeasured_call`, the estimate changes (premium tiers and mantle up, Flex down), and Security-relevant (None, or the deny policy action); check with `go run ./tools/changelogcheck`
- [x] 5.3 Run `go vet ./...` and `go test ./...`, and the release gates

## 6. Live check

- [x] 6.1 Deploy to e2e with the AI guard on in dry-run for one test account; make one standard and one Priority `Converse` call, one Priority `ConverseStream` call, and one `ChatCompletions` call to `openai.gpt-oss-20b-1:0`; check the account's AI totals grew by each call's tier price for its tokens; turn the AI guard off again and remove any test setup
- [ ] 6.2 After this release is deployed to the enterprise's publishing guard, let its scheduled publish (or `guardrail prices publish` with this release's CLI) write the first version with tier prices; check the version lists tier prices as added and that the next day's publish writes nothing. Not before: a publishing guard of an earlier release would publish the tiers as removed. That a guard of v0.2.17 reads a chunk with tiers is covered by `TestChunkTiersRoundTrip`, since a guard with the AI guard off doesn't read shared prices
