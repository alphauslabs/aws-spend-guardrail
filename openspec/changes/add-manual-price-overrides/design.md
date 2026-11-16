# Design

## Context

`guardrail prices publish` (cmd/guardrail `pricesCmd`) calls `prices.Fetch` (Price List plus `ListFoundationModels`, joined by `prices.Build`), then `cli.PublishPrices`. That reads the latest version's AI chunk, prints `prices.Diff` (added, raised, lowered, removed), and publishes the table as one chunk (`ai/us-east-1`) through `jennah.Client.PublishPrices`. A publish with no difference writes nothing. Guards read the chunk into `ai.Table` (`source`, `published`, `basis`, `models`), ignoring other fields, and merge it tighten-only, flooring a shared-only model at its provider's highest release price (add-shared-prices D3). The chunk hash is over canonical JSON (add-shared-prices, fixed in 6.2).

## Goals / Non-Goals

**Goals:** one operator prices a missed model once for every guard in the enterprise; manual prices survive routine publishes; each manual price has a recorded reason; nothing here can lower what a guard charges.

**Non-Goals:**
- Prices for models charged by something other than tokens (images, seconds, searches). The table is per 1,000 tokens.
- Regions other than us-east-1, or chunks other than AI.
- Editing manual prices from the console or MCP. Publishing stays a CLI step.

## Decisions

**D1. File format.** JSON: `{"models": {"<model id>": {"input_per_1k": N, "output_per_1k": N}}}`. Unknown fields are refused (`DisallowUnknownFields`). Model ids are normalized with `ai.ModelKey`, so `us.xai.grok-4.6` and `xai.grok-4.6` are one model, and a file naming both is refused. Both prices must be above zero. A single format keeps the file reviewable in a repository; a price per line flag was rejected as hard to audit.

**D2. Merge with the built table.** For each manual model: if the build priced it, publish `max(built, manual)` for input and output separately, and print "Price List is higher" for each side it kept. Otherwise add it. The published table's `source` is unchanged, and `basis` gains " Some prices were set by hand; see manual." when any are manual.

**D3. Manual prices live in the chunk.** The AI chunk document gains `manual`: a map of model id to `{input_per_1k, output_per_1k, set_in_version}`, the operator's prices as given (not merged). Guards decode into `ai.Table`, which ignores the field, so no guard change and no layout version bump. On publish, the CLI starts from the latest chunk's `manual`, applies the file (an entry replaces the model's previous manual price), then removes each `--drop-override` model (an unknown model there is an error). Rejected: a separate `manual_prices` table. It would need a schema change, a reader-reach change, and a second read on every publish, for a few entries.

**D4. A manual change needs a note.** The CLI compares the resulting `manual` with the latest one. If any entry was added, changed, or dropped and `--note` is empty, it fails before publishing: "manual prices changed; give --note with the reason". A publish that only carries manual prices forward needs no note.

**D5. Diff and warnings.** The diff prints manual models with "(manual)". When the Price List now prices a manual model, it prints "<model>: the Price List now prices it at X; the higher price is published. Drop the manual price with --drop-override <model> if it is no longer needed." For a manual model the release table doesn't list, it computes the guard's floor (the release's highest price for the model's provider, or for any model if the provider has none) and warns when either manual price is below it: "guards will price <model> at no less than <floor>". The floor uses the release table embedded in the CLI build, the same table a guard of that release uses.

**D6. Same-content rule.** The chunk now includes `manual`, so carrying the same manual prices forward with an unchanged build yields the same canonical document, and publish writes nothing as today.

## Risks / Trade-offs

- An operator can raise a model's price far above its real price by mistake, and all guards then overestimate it. The note, the version history, and the diff make this visible; fixing it needs a release for a model the release table lists (tighten-only), or a dropped manual price for a shared-only model.
- A manual price for a model that is still not in the Bedrock model list is published anyway; that is the point for models the list names differently.
- Manual prices are kept per enterprise. An MSP with several enterprises sets them in each.

## Migration Plan

None. Versions published before this change have no `manual` field, which reads as no manual prices.
