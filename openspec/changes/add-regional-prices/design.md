# Design

## Context

Today the builder (`internal/prices/fetch.go`, `build.go`) reads the Price List filtered to `regionCode=us-east-1` and Bedrock's model list in the caller's region, and builds one `Table` of per-model prices. Publishing (`internal/prices/publish.go`) writes it as the single AI chunk `ai/us-east-1` of a version; manual prices ride in that chunk's `manual` field. Guards (`internal/guard/prices.go`) hold that chunk in state as meta `prices.ai.us-east-1` and merge it tighten-only with the release table (`internal/packs/ai`), which prices every call regardless of its region.

The shared-prices storage already keys chunks by pack and region (add-shared-prices D1), and guards of every release so far read only `ai/us-east-1`, ignoring the manifest's `Layout`.

Facts checked on 2026-11-17 (to go into `docs/aws-facts.md`):

- Bedrock is in 33 commercial regions (36 with GovCloud and the EU sovereign cloud). The e2e payer has 18 enabled and 16 not.
- Per-region Price List entries for Bedrock: 650 to 1,700 per region across `AmazonBedrock` and `AmazonBedrockFoundationModels` (8 regions sampled).
- Regional prices against us-east-1, median per region: us-west-2 1.00x, ca-central-1 1.00x, ap-south-1 1.18x, eu-central-1 1.20x, ap-northeast-1 1.20x, sa-east-1 1.20x (up to 1.70x), eu-west-2 1.55x. Mantle prices: median 1.20x, up to 1.60x.
- AWS bills a cross-region inference call at the price of the region it was called from; global profiles cost about 10% less than geographic ones ([cross-region inference](https://docs.aws.amazon.com/bedrock/latest/userguide/cross-region-inference.html)).
- `ListFoundationModels` and the mantle `/v1/models` list AWS's catalog for a region, not what an account has enabled (the e2enormal mantle list included Claude, which isn't enabled for it).
- The Price List names mantle usage types by mantle model id (`USE1-xai.grok-4.3-mantle-input-tokens-standard`), and prices OpenAI's dated snapshots only under the base model's name (`OpenAI GPT-5.4`).
- The guard's function has a 120-second timeout and 256 MB.

## Goals / Non-Goals

**Goals:**
- Every model a payer org's accounts can call through `bedrock-runtime` or `bedrock-mantle` is priced, at the price of the region it's called in, or reported the day after it appears.
- Guards of earlier releases keep working, unchanged, on the same published versions, and rollback stays safe.
- Shared prices still only tighten.

**Non-Goals:**
- Per-region config overrides or manual prices: both stay per model, applied in every region.
- Global-profile discounts: a global profile call is priced at the regional price (about 10% high).
- Batch, flex, priority, provisioned, prompt-cache, and long-context rates: unchanged (not priced).
- GovCloud and the EU sovereign cloud: other partitions, not reachable from a commercial publisher.

## Decisions

### D1: Tables are per region, in the current per-model format

A built table becomes a `RegionalTable`: `source`, `published`, `basis`, and `regions`, a map from region code to the current per-model `models` map. Each region holds the models the Price List prices there. The release's `prices.json` takes this format, and `ai.Prices()` reads it.

### D2: One chunk per region; the us-east-1 chunk stays readable by older guards

Publishing writes one AI chunk per region, `ai/<region>`, each a `Table` as today with that region's models. The `us-east-1` chunk additionally lists every model not offered in us-east-1 at its highest price in any region, and still carries `manual`. A v0.2.4 guard reads `ai/us-east-1` only and so gains prices for regional-only models; it keeps pricing everything at us-east-1 prices until upgraded. New guards ignore the extra models in the us-east-1 chunk: a model not offered in us-east-1 has no us-east-1 price of its own, and D4's fallback gives the same highest price anyway.

The manifest's `layout_version` stays 1, since the layout of each chunk doesn't change and no reader checks it; the change is more chunks.

About 86 models in up to 33 regions: each chunk is about 8 KB, a version about 250 KB, in 3 MiB commits as today (add-shared-prices D1).

### D3: Guards hold every AI chunk, each in its own state item

A guard reads the manifest as today and downloads each `ai/<region>` chunk whose hash differs from the one it holds, keeping each as meta `prices.ai.<region>` (the existing item for us-east-1 is reused). A chunk that fails its hash or doesn't parse is ignored and alerted once, and the guard keeps its previous copy of that region. A region dropped from the manifest is dropped from state.

Fetching only regions with activity was considered (add-shared-prices D3 allows it); with chunks this small, holding all is simpler, and a call in a new region is priced at once.

### D4: Pricing a call: region first, then the model's highest

`ai.Pack.Price(model, region)`:

1. A config override for the model, in any region.
2. The release and shared prices for the model in that region, merged tighten-only, as today.
3. Otherwise, the model's highest price in any region, release or shared.
4. Otherwise, for a model only shared prices list, the provider floor, as today. The floor is the provider's highest release price in that region, or in any region when the region has none.
5. Otherwise, no price: the unknown-model policy applies.

The region is the trail record's `awsRegion`, which for a cross-region inference call is the region it was made from, as AWS bills. Mantle usage (add-mantle-usage) uses the region it was read from. `ai.unknown_models` `count` keeps using the highest prices known, now across all regions.

### D5: Building every region

The builder:

1. Lists the regions the Price List has Bedrock entries for (`GetAttributeValues` for `regionCode`), minus other partitions (`us-gov-`, `eusc-`).
2. Reads the Price List per region (`regionCode` filter, both service codes), as today for us-east-1, two regions at a time with adaptive retries: the Price List API throttles beyond that.
3. In each of those regions, calls `ListFoundationModels` and the mantle endpoint's `GET /v1/models` (SigV4, service `bedrock-mantle`), up to 8 regions at a time. A region whose list fails, including one the account hasn't enabled, is named in the output and priced as step 5 says; a region with no mantle endpoint (its host doesn't resolve) is not a failure. Trying every region avoids a dependency and a permission for listing enabled regions.
4. Prices each listed model in each region from that region's entries, with the existing matching, plus: a mantle id matches usage types named by it (the existing `-mantle` handling, keyed by the full id); a dated snapshot `<base>-YYYY-MM-DD` takes its base model's price; `moonshotai.<x>` takes `moonshot.<x>`'s price when it has none of its own.
5. In regions it couldn't list, prices the models it listed elsewhere when that region's entries price them.

A model listed but without a price anywhere is reported as unpriced, once, as today. A model with a price in some regions only is not reported: D4 prices the other regions at its highest.

### D6: Time and permissions

Measured from the e2e payer on 2026-11-17 (task 1.1): the Price List has Bedrock entries for 36 regions, 23,035 in all (264 pages). Read one region at a time it took 49 seconds; two at a time, 27 seconds; four at a time, 37 seconds (throttled); eight at a time failed for 20 regions after 3 retries. The model lists of the 18 enabled regions took 5 seconds at 8 at a time; mantle answered in 14 of them, and in the other 4 its host doesn't resolve. A full build is about 35 seconds. In the first full run through `guardrail prices build`, the Bedrock endpoint of me-central-1, which the payer hasn't enabled, never accepted a connection, and the build took 2 minutes; each region's model lists now have 15 seconds, and the build takes 43 seconds. It priced 109 models (86 before) in 34 regions; the 16 regions it couldn't list are exactly those the payer hasn't enabled. The function's timeout goes from 120 to 300 seconds, as the scheduled publish shares it and the Price List grows; the minute ticks are unaffected.

The publishing role gains `bedrock-mantle:ListModels`, whose resource is a mantle project, limited to the account's projects (`arn:${AWS::Partition}:bedrock-mantle:*:${AWS::AccountId}:project/*`), in the existing price-publishing policy. Template revision 4.

### D7: Manual prices across regions

Manual prices stay per model, stored in the `us-east-1` chunk's `manual` field. In each region chunk, a manual model's price is the higher of the manual price and that region's Price List price, per side; a region without a Price List price uses the manual price. A manual price is auto-dropped (the existing rule) only when the Price List prices the model at or above it, both sides, in every region the builder found the model offered in. The stale-manual alert fires when any region's Price List price is below the manual price.

### D8: Reporting

`guardrail prices build` and `publish` print, per model, its price range across regions and the regions it's priced in, and the regions whose model lists couldn't be read. The scheduled publish's change alert summarizes per model: added in N regions, raised in N regions (with the largest rise), lowered, removed. Reports stay one alert per publish.

## Risks / Trade-offs

- **A bigger first publish.** The first regional version adds many region chunks and changes no us-east-1 price; its change alert would list every model as added in other regions. The alert says so in one line per model, and the note says it's the first regional version.
- **Release table size.** `prices.json` grows from about 8 KB to about 250 KB in the binary; acceptable.
- **Matching rules grow.** Each new naming quirk adds a rule. The scheduled publish reports what stays unpriced, so a new quirk shows up as a report, not a silent gap.
- **Rollback** to v0.2.4: the guard reads the `us-east-1` chunk, which keeps its meaning, and prices at us-east-1 again. The `prices.ai.<region>` items for other regions are ignored. A v0.2.4 publisher writes versions with only the `us-east-1` chunk; newer guards then price other regions from their release table, which is regional too, so no upgrade order is needed.
