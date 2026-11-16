# Proposal

## Why

The 2026-11-16 build reported 9 Bedrock models with no price. Checking them against the AWS Price List showed that 4 of them are priced there, and the builder misses them because of how AWS lists them:

- `xai.grok-4.6` is listed under its full model id with no `inferenceType`, so its rows are skipped.
- `mistral.voxtral-small-24b-2507`: AWS's standard usage types mislabel it `Voxtral-Mini-24B-2507`, and the rows that carry the real id end in `-mantle`, which the builder doesn't strip.
- `amazon.nova-2-sonic-v1:0` and `amazon.nova-2-5-sonic` have speech and text token rates whose `inferenceType` is "Speech Understanding input token" or "Text Input Token", which the builder rejects. "Nova Sonic 2.0" also orders its words differently from Bedrock's "Nova 2 Sonic".

Each one trips as an unknown model in every guard until someone prices it by hand, although the price is already published by AWS. With the scheduled publish, a builder fix reaches every guard in the enterprise within a day, without a release.

## What Changes

- The builder reads an entry's direction (input or output) from its `inferenceType` when that names input or output tokens in any wording, and from its usage type when `inferenceType` is missing.
- Model keys from a usage type drop a `-mantle` suffix and the speech and text modality words; a model attribute that is a model id is keyed like an id.
- When no key matches exactly, the builder tries a word-order-insensitive key, and uses it only when exactly one Price List model has it.
- A model with several rates for one side (such as speech and text tokens) is priced at the highest regional rate, as for any model today.
- The fix only adds matches and rates; it never lowers a price the builder produced before. Models the Price List doesn't list (Kimi K3.1, GLM 5.3) or doesn't price per token (Rerank 3.5, Pegasus 1.2 and 1.5) stay unpriced.
- The release's embedded table is rebuilt with the 4 models.

## Capabilities

### New Capabilities

### Modified Capabilities

- `shared-prices`: the builder prices every model the Price List prices per token, however AWS labels it, and never lowers an earlier price by the fix.

## Impact

- `internal/prices/build.go`: direction, keys, and the fallback key; `internal/prices/testdata/sample.json`: the Price List rows of the 4 models.
- `internal/packs/ai/prices.json`: rebuilt. Guards of the next release price the 4 models from their release table; earlier guards get them through shared prices.
- Docs: the CHANGELOG.
