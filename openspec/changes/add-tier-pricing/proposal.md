# Proposal

## Why

The AI guard prices every Bedrock call at its model's standard rate. AWS now lets a call choose a service tier, and Priority and Ultrafast calls cost about 2 to 6 times the standard rate (GPT-6 Astra: $11 and $55 per 1M tokens standard, $66 and $330 Ultrafast). An account calling on those tiers can spend several times its cap before the guard trips, which is the one way the guard currently errs toward blocking too late. A test on 2026-11-24 showed that CloudTrail records the tier a `Converse` call used (`requestParameters.serviceTier.type`), so the guard can price it.

## What Changes

- The price builder keeps each model's Flex, Priority, and Ultrafast rates as tier prices, next to its standard price, instead of dropping them. The standard price is still built from standard rows only, so it doesn't change.
- Shared price chunks and the release's price table carry tier prices per model and region. Guards of earlier releases ignore them and keep pricing at the standard rate.
- Tier prices merge tighten-only, per tier, as standard prices do.
- The AI guard prices each call at the tier its record shows. A record that shows no tier is priced at the standard rate.
- A call whose record can't show a tier (mantle usage, and any runtime call kinds the live checks show don't record it) is priced at its model's highest tier price, so the guard keeps blocking first.
- A tier the guard doesn't know is priced at the model's highest tier price and reported once, with an alert and an `unpriced_tier` event.
- An `ai.prices` entry sets a model's standard price. A call on another tier is priced at the higher of the entry and the tables' price for that tier, so an override can't make a premium tier cheap.
- Publishing reports tier price changes like standard ones, naming the tier.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `ai-pack`: calls are priced at the service tier they used; calls whose tier can't be told and unknown tiers are priced at the model's highest tier; how `ai.prices` applies to tiers.
- `shared-prices`: the builder keeps tier rates as tier prices instead of folding or dropping them; tier prices are published per region and merged tighten-only.

## Impact

- `internal/prices` (build, publish, diff), `internal/packs/ai` (price table, lookup, evaluation), `internal/activity` (record parsing), `internal/packs/ai/prices.json` (regenerated with tiers), and `internal/cli` (publish output).
- Shared price chunks gain a `tiers` field. The layout version stays the same: older guards ignore unknown fields in a chunk.
- Estimates rise for calls on premium tiers and for mantle usage of models that offer premium tiers. They fall for Flex calls, which are priced at the Flex rate.
- `docs/ai-guard.md`, `docs/operating.md`, and `docs/aws-facts.md` (which call records carry the tier, and how).
