# Proposal

## Why

The AI guard prices every Bedrock call at its us-east-1 price, and only knows the models Bedrock lists in us-east-1. Both are wrong for an organization working elsewhere:

- **Prices differ by region.** The Price List on 2026-11-17, compared with us-east-1 (median of each region's token prices): us-west-2 1.00x, ap-south-1 1.18x, eu-central-1 and ap-northeast-1 1.20x, eu-west-2 1.55x, and up to 1.70x in sa-east-1. Mantle prices outside us-east-1 run 1.20x (median) to 1.60x. AWS bills a cross-region inference call at the price of the region it was made from. So an account with a $100 cap working in London is cut off after about $155 of spend.
- **Models offered only outside us-east-1 have no price.** `deepseek.v3-v1:0`, `qwen.qwen3-235b-a22b-2507-v1:0`, `qwen.qwen3-coder-480b-a35b-v1:0`, and `mistral.mistral-large-2407-v1:0` are offered in 1 to 7 other regions; a call to one trips `unknown_model` and, by default, cuts the account off. The daily publish never reports them, because it never sees them.
- **Mantle model ids aren't priced.** 14 of the 62 models the `bedrock-mantle` endpoint lists have no price, though the Price List prices 13 of them, under usage types named by the mantle id (`USE1-google.gemma-4-26b-a4b-mantle-input-tokens-standard`), under a base model (`openai.gpt-5.4-2026-03-05` as GPT-5.4), or under another provider prefix (`moonshotai.kimi-k2-thinking` as `moonshot.kimi-k2-thinking`). Counting mantle spend (add-mantle-usage) needs them.

AWS adds models and regions often. The daily scheduled publish should price every model a payer org's accounts can call, at the price of the region they call it in, and report the rest the day after AWS adds them, without a release.

## What Changes

- The price builder reads the Price List for every Bedrock region and the model lists of every region the publishing account has enabled, Bedrock's and the `bedrock-mantle` endpoint's, and builds a price per model and region. It prices mantle model ids, dated snapshots through their base model, and `moonshotai.` as `moonshot.`.
- A price version holds one AI chunk per region. The `us-east-1` chunk keeps its current meaning for guards of earlier releases, which read only that chunk, and also lists models not offered in us-east-1 at their highest regional price, so those guards stop tripping on them.
- The release's built-in table carries prices per region too.
- Guards price each call at the price of its region: the call's region for trail records, and the region read for mantle usage. A model with no price in a region falls back to its highest price in any region. Config overrides and manual prices apply in every region, and shared prices still only tighten.
- A manual price is dropped by itself once the Price List prices the model in every region at or above it.
- The scheduled publish reports models still without a price in any region, and changes summarized per model across regions.
- The guard's function gets a longer timeout, and the publishing role a permission to list mantle models. Template revision 4.

## Capabilities

### New Capabilities

### Modified Capabilities

- `shared-prices`: chunks per region, the builder reading every region and mantle ids, and manual prices applying in every region.
- `ai-pack`: each call priced at its region's price.

## Impact

- `internal/prices` (fetch, build, publish, manual), `internal/jennah` (chunks per region), `internal/guard` (pull and hold a chunk per region), `internal/packs/ai` (regional lookup), `internal/cli` (`prices build` and `publish` output).
- `internal/packs/ai/prices.json` regenerated with regions; its format changes.
- `guard/template.yaml`: timeout, `bedrock-mantle:ListModels` for the publishing role, revision 4.
- Docs: `ai-guard.md` (how prices work), `operating.md` (manual prices and publishing), `aws-facts.md` (regional prices, cross-region billing, mantle Price List naming), `upgrade.md`; CHANGELOG.
- Guards of earlier releases keep working on the `us-east-1` chunk, still at us-east-1 prices, until upgraded.
- add-mantle-usage, not yet applied, then takes its prices from here: its model-id matching (design D4) shrinks to what the builder doesn't resolve, and its template revision becomes 5.
