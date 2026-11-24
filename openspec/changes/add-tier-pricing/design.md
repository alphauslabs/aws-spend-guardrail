# Design

## Context

See proposal.md for why. The current state:

- The price builder (`internal/prices/build.go`) skips any Price List row whose usage type or `inferenceType` contains a word in `skipUsage` (batch, reserved, long_ctx, flex, priority, ultrafast, cache, latency, tpm), and prices each side at the highest remaining rate. Ultrafast was added to the list in v0.2.16, after its rates were taken as standard ones.
- A model's price is `config.ModelPrice{InputPer1K, OutputPer1K}`. A region's chunk and the release table (`internal/packs/ai/prices.json`) hold `models` (and the release, `regions`). Guards parse chunks with plain `json.Unmarshal`, so they ignore fields they don't know. Config parsing is strict, but config isn't touched here.
- The AI pack (`internal/packs/ai/ai.go`) prices a call with `PriceIn(key, region)`: the org's `ai.prices` entry first, then release and shared prices merged tighten-only (`higher`), then the model's highest price in any region.
- `activity.Record` parses `requestParameters.modelId` only. Mantle usage comes from metrics (`CategoryMetric`) and names no tier.
- Observed on 2026-11-24 (e2e payer, `openai.gpt-oss-20b-1:0`): `Converse` with `serviceTier` `priority` and `flex` was recorded with `requestParameters.serviceTier.type` set to the tier, next to `modelId` and `inferenceConfig`. Nova Micro refused both tiers (`ValidationException`), and the refused calls were recorded with the tier too. A call without a tier has no `serviceTier`. Not yet observed: `InvokeModel` (which takes the tier in a header), `ConverseStream` and `InvokeModelWithResponseStream` completion records, and how Ultrafast is requested.

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

### D3: The record names the tier, and some record kinds are tier-blind

`activity.Record` gains `ServiceTier`, from `requestParameters.serviceTier.type`, lowercased. The AI pack keeps a fixed set of tier-blind record kinds: usage read from metrics always, and each runtime call kind the live checks (task 1) show never carries the tier. A record of a tier-blind kind is priced as D4's "can't tell". A record of another kind with no tier is standard.

The set is fixed in code from observed records, with the dates in `docs/aws-facts.md`, so a change at AWS shows up when the facts are rechecked.

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

## Risks / Trade-offs

- [If `InvokeModel` doesn't record the tier, every `InvokeModel` call to a model with premium tiers is priced at its highest tier, up to 6 times its standard price, and trips caps early.] → Task 1 checks this first. If it's tier-blind, the alert for a model priced this way says so, and the docs say Converse records the tier. An org that calls such models heavily through `InvokeModel` can raise its caps.
- [Mantle usage of models with premium tiers is priced at the highest tier from now on.] → Same block-first trade-off. The docs say so, and the release notes name it.
- [An `ai.prices` entry can't lower a premium tier.] → By design, so a config change can't make premium calls cheap. An org that needs a lower tier price waits for the Price List or a release.
- [AWS adds a fourth tier with a new word.] → Rows with an unknown word are standard rows today and would raise the standard price, which v0.2.17's 2x note points out. Calls naming that tier are priced at the highest known tier and reported once (D5). Adding the word is a builder change.
- [The first publish with tiers is a large version, and its email lists many added tier prices.] → Its note says it adds tier prices; the operating docs mention it.

## Migration Plan

1. Release with the builder, the chunk field, and the guard lookup. The release table gains `region_tiers`.
2. The first publish after upgrading writes a version with tier prices. Older guards read it at standard prices; upgraded guards price tiers from then on.
3. Rollback: install the previous release. It ignores `tiers` and prices at the standard rate, as before.

## Open Questions

- How an Ultrafast call is requested and recorded. If it uses `serviceTier` `ultrafast`, D3 covers it; if it uses another field, task 1 adds it to the parsing. Either way the specs and tasks hold.
