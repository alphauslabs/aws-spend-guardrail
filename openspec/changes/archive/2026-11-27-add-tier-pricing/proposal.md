# Proposal

## Why

The AI guard prices every Bedrock call at its model's standard rate. AWS now lets a call choose a service tier, and Priority and Ultrafast calls cost about 2 to 6 times the standard rate (GPT-6 Astra: $11 and $55 per 1M tokens standard, $66 and $330 Ultrafast). An account calling on those tiers can spend several times its cap before the guard trips, which is the one way the guard currently errs toward blocking too late. Tests on 2026-11-24 showed that CloudTrail records the tier on every runtime call kind (in `requestParameters` or `additionalEventData`), so the guard can price it. They also showed a gap that predates tiers: calls to Bedrock's OpenAI-compatible API on `bedrock-runtime` are recorded as `ChatCompletions` and `Responses`, with token counts, and the guard doesn't price them at all. Ultrafast can only be requested through that API.

## What Changes

- The price builder keeps each model's Flex, Priority, and Ultrafast rates as tier prices, next to its standard price, instead of dropping them. The standard price is still built from standard rows only, so it doesn't change.
- Shared price chunks and the release's price table carry tier prices per model and region. Guards of earlier releases ignore them and keep pricing at the standard rate.
- Tier prices merge tighten-only, per tier, as standard prices do.
- The AI guard prices `ChatCompletions` and `Responses` calls on `bedrock-runtime` like the other runtime calls, against the same caps, allowlists, and unknown-model policy.
- A streamed `ChatCompletions` call leaves no token counts in CloudTrail, so the guard can't measure it. Such a call trips a new `unmeasured_call` rule and cuts the account off, unless the org's config sets `ai.unmeasured_calls` to `allow`; then it is counted as a call at $0 and reported once. Measuring these calls from CloudWatch metrics is a separate, later change.
- The AI guard prices each call at the tier its record shows. A record that shows no tier is priced at the standard rate. A `ConverseStream` completion takes its tier from the `ConverseStream` call record it belongs to.
- Usage whose record can't show a tier (mantle usage from metrics, and a `ConverseStream` completion whose call record the guard hasn't seen) is priced at its model's highest tier price, so the guard keeps blocking first.
- A tier the guard doesn't know is priced at the model's highest tier price and reported once, with an alert and an `unpriced_tier` event.
- An `ai.prices` entry sets a model's standard price. A call on another tier is priced at the higher of the entry and the tables' price for that tier, so an override can't make a premium tier cheap.
- Publishing reports tier price changes like standard ones, naming the tier.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `ai-pack`: OpenAI-compatible runtime calls are priced; calls are priced at the service tier they used; calls whose tier can't be told and unknown tiers are priced at the model's highest tier; how `ai.prices` applies to tiers.
- `guard-config`: the `ai` section gains `unmeasured_calls` (`cutoff` or `allow`), validated and classified like `unknown_models`.
- `shared-prices`: the builder keeps tier rates as tier prices instead of folding or dropping them; tier prices are published per region and merged tighten-only.

## Impact

- `internal/prices` (build, publish, diff), `internal/packs/ai` (price table, lookup, evaluation), `internal/activity` (record parsing), `internal/packs/ai/prices.json` (regenerated with tiers), and `internal/cli` (publish output).
- Shared price chunks gain a `tiers` field. The layout version stays the same: older guards ignore unknown fields in a chunk.
- Spend through the OpenAI-compatible runtime API starts counting toward caps, so accounts that use it reach their caps sooner than before. An account that streams Chat Completions is cut off on its first streamed call after the upgrade, unless its org allows unmeasured calls first. **BREAKING** for such accounts.
- Estimates rise for calls on premium tiers and for mantle usage of models that offer premium tiers. They fall for Flex calls, which are priced at the Flex rate.
- `docs/ai-guard.md`, `docs/operating.md`, and `docs/aws-facts.md` (which call records carry the tier, and how).
