# Proposal

## Why

A Bedrock call to a model the guard has no price for trips the `unknown_model` tripwire. In enforce mode, that cuts off the account's Bedrock and AgentCore access at once. A client that tries a model released this week, or one the price builder can't match (9 models in the 2026-11-15 build, including Grok 4.6 and GLM 5.3), loses all AI access until someone prices the model and lifts the cutoff. That is the safest outcome for spend but the harshest for users, and an MSP may prefer to keep the client working, cap the spend, and be told.

## What Changes

- A new `ai.unknown_models` setting, per payer org: `cutoff` (today's behavior, and the default) or `count`.
- With `count`, a call to an allowed model that has no price is counted against the account's caps at the highest input and output price the guard knows. A model is never priced cheaper than any priced model. The account's caps then decide, as for any other call.
- With `count`, a call to an unpriced model whose record carries no token counts still trips the tripwire, since it can't be estimated at all.
- The first counted call to each unpriced model raises one alert and one `unpriced_model` inbox event naming the model, the price used, and how to price it. Later calls to that model don't repeat them.
- Moving from `cutoff` to `count` is a loosening that needs approval; moving back is a tightening.
- The model and region allowlists, the API-key tripwire, and the logging tripwire are unchanged. A model not on an allowlist still trips `model_not_allowed`.

## Capabilities

### New Capabilities

### Modified Capabilities

- `ai-pack`: an unpriced model trips or is counted at the highest known price, by config, and a counted one is reported once (alert and `unpriced_model` event).
- `guard-config`: the `ai` section takes `unknown_models`, validated and classified.

## Impact

- `config`: the new field in the `ai` section, its JSON schema, validation, resolution, the diff and its approval sentence.
- `internal/packs/ai`: pricing an unknown model at the ceiling under `count`, and reporting it.
- `internal/activity` and `internal/guard`: the once-per-model alert and event, kept in state.
- `schema`: the new event kind. Docs: `docs/ai-guard.md`, `docs/operating.md`, CHANGELOG.
- Out of scope: Bedrock calls the AI guard doesn't watch at all today (`InvokeModelWithBidirectionalStream` used by Nova Sonic, async invoke, and Rerank). They are neither priced nor tripped; that gap needs its own change.
