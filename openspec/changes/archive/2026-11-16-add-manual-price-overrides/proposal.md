# Proposal

## Why

`guardrail prices publish` builds the shared AI price table only from what it can match between the AWS Price List and Bedrock's model list. Models it can't match stay unpriced for every guard in the enterprise, and today the only fix is an `ai.prices` override in each payer org's config. That means one loosening approval per org, up to a thousand orgs per MSP, for a price one operator could set once. The 2026-11-15 build missed 9 models, including Grok 4.6, GLM 5.3, and Kimi K3.1.

## What Changes

- `guardrail prices publish` takes `--overrides FILE`, a JSON file of model prices per 1,000 input and output tokens, validated strictly.
- An override for a model the Price List doesn't price adds it to the shared table. An override for a model the Price List prices is published only where it is higher, input and output separately, and publish says when the Price List is higher.
- Manual prices are recorded in the published version and carried into later versions, so a publish without the file keeps them. `--drop-override MODEL` removes one. When the Price List later prices a manual model, publish keeps the higher price and suggests dropping the override.
- Adding, changing, or dropping a manual price requires `--note`, so every manual price has a reason in the version history.
- The diff output marks manual prices, and warns when a manual price is below what guards will actually use (a shared-only model is priced at no less than its provider's highest release price).
- Guards are unchanged: shared prices stay tighten-only, so a manual price can raise or add a price but never lower one.

## Capabilities

### New Capabilities

### Modified Capabilities

- `shared-prices`: operators can price models by hand when publishing, and those prices carry forward. This capability is introduced by add-shared-prices, which should be archived first.

## Impact

- `internal/prices`: reading and validating the overrides file, merging it with the built table.
- `internal/cli` (`PublishPrices`) and `cmd/guardrail`: the new flags, carrying manual prices forward from the latest version, the note rule, and the diff marks.
- The AI chunk document gains a `manual` field. Guards ignore it, so no layout version change and no guard release.
- Docs: `docs/operating.md` (Shared AI prices), `docs/ai-guard.md` (Prices), CHANGELOG.
