# Tasks

## 1. Facts

- [ ] 1.1 From a linked member account in a listed region, make a streamed Chat Completions call that runs past a minute boundary, and a Priority call; compare the trail records' `eventTime` with the minutes the `AWS/Bedrock` datapoints land in, and check whether a tier downgrade (`ServiceTier` differs from `ResolvedServiceTier`) shows in the trail; record both in `docs/aws-facts.md` and widen D3's window if needed
- [ ] 1.2 Make a `Converse` call with a prompt-cache read, and compare the trail's token counts with the metrics'; record in `docs/aws-facts.md` whether cache tokens are in `InputTokenCount`

## 2. Reading

- [ ] 2.1 Add a runtime metrics source in `internal/awsx` that, given a list of account, model, and window, reads `InputTokenCount` and `OutputTokenCount` for exactly those accounts (`AccountId` per query) and models, per model and per `ResolvedServiceTier`, in one batched `GetMetricData` call per region (design D1, D2); test the queries it builds for two accounts, that it reads nothing for an empty list, and the default-tier split against recorded responses
- [ ] 2.2 Wire it in `guard/main.go` for each region in `MANTLE_REGIONS`, beside the mantle source; test that no region listed reads nothing

## 3. Reconciling

- [ ] 3.1 Record the trail-counted tokens per account, model, tier, and 5-minute window, and the pending gaps (windows with an unmeasured completion), kept until the window is 15 minutes old, in the state table with the trail's other state (design D2, D3); test that a stored file's tokens land in their window once, and that a file with an unmeasured completion adds one pending gap
- [ ] 3.2 Add the reconciler, run every 5 minutes: read each pending window at 10 and 15 minutes old, charge metrics minus trail per account, model, and tier, growth only, never negative, saved only with the usage, and drop the window after its second read (design D3, D4); test a streamed call's gap, a window mixing a measured and an unmeasured call, a late trail record, a negative gap, a store failure, and that no read happens without pending gaps
- [ ] 3.3 Price gap records in the AI pack at their tier with zero calls (design D4); test a Priority gap and a default-tier gap

## 4. Unmeasured calls

- [ ] 4.1 Tell the AI pack which accounts and regions have readable runtime metrics, from the listed regions, the daily link check, and the last read's result (design D5); test that an unmeasured completion there counts one call at $0 without tripping, and trips in an unlisted region, for an unlinked account, and after a failed read
- [ ] 4.2 Extend the unreadable-metrics alert and the daily unlinked-account check to runtime metrics, one report per account and region; test both

## 5. Docs and release

- [ ] 5.1 Update `docs/ai-guard.md` (runtime metrics, the gap, the 10 to 15 minute lag for streamed calls, unmeasured calls) and `docs/install.md` (`MantleRegions` covers runtime metrics too, and the read cost: none without unmeasured calls, about two metrics per tier per account and model with them, every 5 minutes); check style: no em-dashes, Oxford comma
- [ ] 5.2 Add the CHANGELOG entry (package-only upgrade, unmeasured calls now charged where metrics are read, Security-relevant: None); run `go vet ./...`, `go test ./...`, and the release gates

## 6. Live check

- [ ] 6.1 Deploy to e2e with the AI guard on in dry-run for one linked test account and `MantleRegions` listing us-east-1; make a streamed Chat Completions call with `ai.unmeasured_calls` at `cutoff`; check the account isn't cut off, its totals gain the call's tokens at its tier's price within 15 minutes, and the guard's CloudWatch requests in that time name only that account; turn the AI guard off and remove the test setup
