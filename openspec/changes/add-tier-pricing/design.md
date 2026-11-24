# Design

## Context

See proposal.md for why. The current state:

- The price builder (`internal/prices/build.go`) skips any Price List row whose usage type or `inferenceType` contains a word in `skipUsage` (batch, reserved, long_ctx, flex, priority, ultrafast, cache, latency, tpm), and prices each side at the highest remaining rate. Ultrafast was added to the list in v0.2.16, after its rates were taken as standard ones.
- A model's price is `config.ModelPrice{InputPer1K, OutputPer1K}`. A region's chunk and the release table (`internal/packs/ai/prices.json`) hold `models` (and the release, `regions`). Guards parse chunks with plain `json.Unmarshal`, so they ignore fields they don't know. Config parsing is strict, but config isn't touched here.
- The AI pack (`internal/packs/ai/ai.go`) prices a call with `PriceIn(key, region)`: the org's `ai.prices` entry first, then release and shared prices merged tighten-only (`higher`), then the model's highest price in any region.
- `activity.Record` parses `requestParameters.modelId` only. Mantle usage comes from metrics (`CategoryMetric`) and names no tier.
- Observed on 2026-11-24 (e2e payer, `openai.gpt-oss-20b-1:0`, recorded in `docs/aws-facts.md`):
  - `Converse` and the `ConverseStream` call record carry `requestParameters.serviceTier.type`.
  - `InvokeModel` carries `additionalEventData.serviceTier`, though the tier is sent in a header. The `InvokeModelWithResponseStream` completion record carries it in `serviceEventDetails.AdditionalEventData.additionalEntries.serviceTier`, next to the token counts.
  - The `ConverseStream` completion record, which has the token counts, carries no tier. Its `parentRequestId` is the `requestID` of the `ConverseStream` call record. Both records had the same `eventTime`.
  - Calls to the OpenAI-compatible API on `bedrock-runtime` are management events named `ChatCompletions` and `Responses`, with `requestParameters.modelId`, and for `ChatCompletions` `inputTokens`, `outputTokens`, `stream`, and `serviceTier` in `additionalEventData`. The AI pack doesn't price them today.
  - A call without a tier has no tier field. A model refuses a tier it doesn't offer with `ValidationException`, and the refused call is recorded with the tier.
  - Ultrafast is requested only through the OpenAI-compatible API, as `service_tier: "ultrafast"` (the GPT-6 Astra model card). The runtime API's tier values are `priority`, `default`, `flex`, and `reserved`.

## Goals / Non-Goals

**Goals:**
- Price a call at the tier it used wherever the record shows it, and at the model's highest tier wherever it can't.
- Keep standard prices exactly as they are now, so a guard with no tiered traffic sees no change.
- Stay compatible with guards of earlier releases reading the same shared chunks.

**Non-Goals:**
- Batch, provisioned, prompt-cache, latency-optimized, and long-context rates stay out, as now.
- Manual tier prices in `guardrail prices publish --overrides`. Manual prices stay standard-only; a model AWS prices only by hand has no tier prices.
- Caps or allowlists per tier. Tiers change only the price.
- Tier prices in `ai.prices` config.

## Decisions

### D1: Three named tiers, read from the Price List's own words

The builder recognizes `flex`, `priority`, and `ultrafast` as tiers, from the usage type (`..._input_tokens_priority-Units`) or `inferenceType` ("Output tokens ultrafast"). A row naming none of them, or `standard`, is standard. The other `skipUsage` words still drop a row, so a long-context Ultrafast row (`..._long_ctx_ultrafast`) is dropped, not taken as Ultrafast. Each tier is priced like the standard tier: regional rates before global, the highest per side.

Alternative: keep every rate the Price List lists under its raw suffix. Rejected: the names AWS uses vary by provider, and an open set makes the guard's lookup and the publish diff hard to read.

### D2: Tier prices sit beside standard prices, in a new field

A chunk gains `tiers`: model id to tier name to `ModelPrice`. The release table gains `region_tiers`: region to that map. `models` and `regions` keep the standard prices. Older guards ignore the new fields and keep pricing at the standard price; the chunk layout version doesn't change. Chunk hashes cover the new field, so a tier change writes a version.

Alternative: put the tier in the model key (`openai.gpt-6-astra#ultrafast`). Rejected: older guards would read it as a model id and price nothing new, but `guardrail prices` output and the floor logic would treat it as a model.

### D3: The record names the tier, and only metrics are tier-blind

`activity.Record` gains `ServiceTier`, lowercased, from the first of `requestParameters.serviceTier.type`, `additionalEventData.serviceTier`, and the completion record's `additionalEntries.serviceTier`, and `RequestID` and `ParentRequestID`. `default` reads as standard. Usage from metrics is tier-blind and priced as D4's "can't tell". Any other record with no tier is standard.

The fields are fixed in code from observed records, with the dates in `docs/aws-facts.md`, so a change at AWS shows up when the facts are rechecked.

### D4: One lookup with a tier

`PriceIn` gains a tier argument with three cases:

- **Standard** (no tier, on a kind that records tiers): exactly today's lookup.
- **A named tier** the tables price for the model in that region (or, failing that, in any region, at its highest): that price, release and shared merged tighten-only per tier. An `ai.prices` entry raises it to at least the entry, per side.
- **Can't tell, or a tier the tables don't price for the model**: the highest of the model's standard and tier prices in that region, per side. An `ai.prices` entry raises it to at least the entry.

The highest-known price used for unpriced models also takes in tier prices, so an unpriced model is never priced below a known premium tier.

### D5: An unknown tier is reported once

A named tier with no price for the model is reported once per model and tier, kept in the guard's state like `unpriced_model` reports. Its `unpriced_tier` event names the model, the tier, the account, and the price used. Tier-blind records aren't reported, since they're expected.

### D6: Publish diffs tiers like models

The diff compares tier prices per model, tier, and region, and each change line names the tier (`openai.gpt-6-astra ultrafast raised ...`). The 2x note from v0.2.17 applies per tier. The first publish with tier pricing lists every tier price as added; its note says why.

### D7: A ConverseStream completion takes its call record's tier

The engine keeps the tier of each `ConverseStream` call record that names one, by `requestID`, for 15 minutes. A completion record whose `parentRequestId` matches takes that tier. A completion with no match is priced as D4's "can't tell", since a Priority call can't be told from a standard one without its call record. The two records were written in the same second, and the guard reads trail files in order, so a miss should be rare: a call record in a file the guard had already finished, or a call made just before an upgrade.

Alternative: price every `ConverseStream` completion at the highest tier. Rejected: it overcharges every standard streaming call to a model with premium tiers, up to 6 times.

### D8: OpenAI-compatible runtime calls are priced like the others

`ChatCompletions` and `Responses` join the priced runtime calls, read like `InvokeModel`: `modelId`, token counts and tier from `additionalEventData`, the caller, and the region. A streamed call is priced from whichever of its records carries token counts, as for the other streaming calls; task 1.5 checks which. They go through the same caps, allowlists, tripwires, and unknown-model policy. The cutoff already denies `bedrock:InvokeModel`; task 1.4 checks it refuses these calls too, and if it doesn't, adds their action to the deny policy.

### D9: A call with no token counts is unmeasured, and trips unless allowed

A successful record that should carry a priced call's token counts and doesn't is unmeasured: a streaming completion record with no tokens (a streamed `ChatCompletions` call leaves one, named `InvokeModelWithResponseStream`), and a non-streamed `ChatCompletions` or `Responses` call record with no tokens. A `ChatCompletions` or `Responses` call record with `stream: true` is priced from its completion, like the other streaming calls. A successful `Responses` record hasn't been seen (GPT-6 Astra, the model tried, is refused to the e2e accounts), so this covers it whatever AWS records. The AI pack trips `unmeasured_call` on it, naming the model and the caller, unless `ai.unmeasured_calls` is `allow`. Then it counts one call at $0, and the first one per account and model is reported with an alert and an `unmeasured_call` event. The call record, which has no tokens either, is still checked by the tripwires as now and priced at nothing.

The rule keys on the missing tokens, not the call name, so any other call kind AWS records without tokens is caught too. A direct `InvokeModelWithResponseStream` completion carries tokens and isn't affected.

`unmeasured_calls` defaults to `cutoff`. `cutoff` to `allow` is loosening, the reverse tightening, as for `unknown_models`.

Alternatives: count and report without tripping (rejected: streamed spend would stay uncapped, the gap this guard exists to close); fill the gap from CloudWatch runtime metrics (the accurate fix, a separate change).

### D10: A daily check for Ultrafast use the trail didn't show

How an Ultrafast call is recorded hasn't been seen. It is very likely `additionalEventData.serviceTier` `ultrafast`, as a Priority call through the same API was, and D3 reads that. If AWS records it elsewhere, the guard would read no tier and charge the standard price, a sixth of the cost. As a check, once a day in each region listed in `MantleRegions`, the guard reads the day's `AWS/Bedrock` `InputTokenCount` and `OutputTokenCount` where `ResolvedServiceTier` is `ultrafast`, per account and model, with one search query through the existing sink. For each account and model with Ultrafast tokens that the trail recorded no Ultrafast call for that day, it alerts once and writes one `ultrafast_unseen` event, naming them and the tokens. The engine keeps, per day, the accounts and models it saw Ultrafast calls for.

This costs one `GetMetricData` read per region per day, a handful of metrics. Without listed regions, the check doesn't run, and the docs say so.

## Risks / Trade-offs

- [Pricing `ChatCompletions` and `Responses` is a step change for accounts that use that API: spend that never counted starts counting.] → The CHANGELOG and release notes say so plainly, so an MSP can check caps for those accounts before upgrading.
- [Accounts that stream Chat Completions are cut off on their first streamed call after the upgrade.] → The CHANGELOG's Upgrade section says to set `ai.unmeasured_calls` to `allow` first for orgs whose accounts do, and approve it, before updating the stack. The cutoff event says what was unmeasured and how to allow it.
- [A `ConverseStream` completion whose call record the guard missed is priced at the highest tier.] → Rare by D7's reasoning; the cost is an early trip, not a late one.
- [Mantle usage of models with premium tiers is priced at the highest tier from now on.] → Same block-first trade-off. The docs say so, and the release notes name it.
- [An `ai.prices` entry can't lower a premium tier.] → By design, so a config change can't make premium calls cheap. An org that needs a lower tier price waits for the Price List or a release.
- [AWS adds a fourth tier with a new word.] → Rows with an unknown word are standard rows today and would raise the standard price, which v0.2.17's 2x note points out. Calls naming that tier are priced at the highest known tier and reported once (D5). Adding the word is a builder change.
- [The first publish with tiers is a large version, and its email lists many added tier prices.] → Its note says it adds tier prices; the operating docs mention it.

## Migration Plan

1. Release with the builder, the chunk field, and the guard lookup. The release table gains `region_tiers`.
2. The first publish after upgrading writes a version with tier prices. Older guards read it at standard prices; upgraded guards price tiers from then on.
3. Rollback: install the previous release. It ignores `tiers` and prices at the standard rate, as before.

## Open Questions

- How an Ultrafast call and a successful `Responses` call are recorded. D3 and D9 cover the likely answer and the unsafe alternatives, and D10 reports a wrong guess within a day. When an account with GPT-6 Astra access is available, one call each settles it; `docs/aws-facts.md` marks both "to confirm".
