# Proposal

## Why

Shared AI prices change only when someone runs `guardrail prices publish`. If nobody does, guards keep stale prices: AWS's new models stay unpriced (and trip or are counted at the ceiling), and price rises go uncounted, with no signal that the table is behind. An enterprise needs the table kept current without relying on a person to remember.

## What Changes

- A new guard task, `publish_prices`, run daily by a schedule the stack adds only when the new parameter `PublishSharedPrices` is `true`. It builds the table from the Price List and publishes it exactly as `guardrail prices publish` does, carrying manual prices forward. On most days nothing changed and nothing is written.
- The operator turns it on in one payer org per enterprise. Turning it on in two is harmless, since a publish with no change writes nothing.
- A third Jennah key, the price publisher, stored in Secrets Manager and named by the new parameter `PricePublisherSecretArn`. It can read and write `guardrail.prices` only. The guard reads it only for this task, and reports and warns on its expiry like the reader and writer keys.
- Local alerts: when a scheduled publish fails (once per failing stretch, and once when it works again); when a published version adds, raises, or lowers prices (one summary); and when the build finds a model with no price that the previous build priced or didn't list (so an operator can price it by hand).
- A manual price the Price List now covers is cleaned up. When the Price List prices the model at or above the manual price on both input and output, every publish (scheduled or by CLI) drops the manual price by itself, since that changes no published price; the version note says so. When the manual price is higher on either side, it is kept, and a scheduled publish alerts once naming the model, both prices, and the command to drop it.
- `guardrail setup` prints the publisher key's reach next to the guard keys.
- With the parameter off, the guard is unchanged: no schedule, no extra permission, and no third key.

## Capabilities

### New Capabilities

### Modified Capabilities

- `shared-prices`: prices can be published on a schedule by one guard, with alerts on failure, on changes, on newly unpriced models, and on manual prices above the Price List; a manual price the Price List covers at or above it is dropped automatically.
- `guard-jennah-contract`: a guard that publishes prices holds a third key, limited to the prices dataset, with its expiry reported and warned.

## Impact

- `guard/template.yaml`: two parameters, a condition, the schedule, and the conditional grants (Price List and Bedrock model list read, the publisher secret).
- `guard/main.go` and `internal/guard`: the task, its state and alerts, and the third key in the daily expiry check.
- `internal/cli` and `internal/prices`: the publish logic shared by the CLI and the task.
- `schema`: the publisher key's reach, printed by `setup`. Docs: `docs/operating.md`, `docs/ai-guard.md`, CHANGELOG.
- Depends on add-manual-price-overrides (carrying manual prices forward), which must be archived before this change is, since this change modifies one of its requirements.
