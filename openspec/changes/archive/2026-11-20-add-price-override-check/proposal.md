# Proposal

## Why

A payer org's `ai.prices` entry wins over every other price the guard knows, and nothing ever retires it. The operating guide now suggests one to charge a new model's actual price while the release's provider floor would overcharge it. Once AWS raises that model's price, or a release or shared price lists it higher, the old entry keeps the lower price, and the org's caps trip late without anyone noticing. Shared manual prices drop themselves when the Price List covers them; config prices don't, so the guard should at least say when one has fallen below what it otherwise knows.

## What Changes

- Once a day, when the AI guard is on, the guard compares each `ai.prices` entry with the model's price from its release table and shared prices, without the provider floor. When the entry is below that price on input or output in every region that prices the model, the guard alerts once and writes one `price_override_low` event naming the model, the entry, and the known price, and saying how to fix it.
- It doesn't repeat for the same entry and known price. A changed entry or known price that is still low is reported again. An entry raised, removed, or no longer low is forgotten silently.
- Nothing changes in how calls are priced: the entry still wins until the org's config changes it.
- The operating guide and the AI guard doc say the guard reports a config price that has fallen below the known price.

## Capabilities

### New Capabilities

### Modified Capabilities

- `ai-pack`: a config price below the price the guard otherwise knows is reported.
- `guard-jennah-contract`: the inbox gains the `price_override_low` event kind.

## Impact

- `internal/packs/ai` (the model's known price without the floor or the config entry), `internal/guard` (the daily check and its reported records), `schema` (the event kind).
- No template or permission change. Rolling back is safe: an older guard doesn't check, and ignores the reported records.
- Jennah console (`app/`): a label and summary for `price_override_low` in the events table, as a separate console change.
- `docs/operating.md`, `docs/ai-guard.md`; CHANGELOG.
