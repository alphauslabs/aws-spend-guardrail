# Design

## Context

`ai.Pack.Evaluate` (internal/packs/ai/ai.go) checks, on each Bedrock call record, the API-key, region, and model allowlist tripwires, then `Price(key)`. With no price it returns a `Hit` with rule `unknown_model`, which the activity engine turns into a cutoff (enforce) or would-cutoff (dry-run) for the account. `Price` looks up the org's `ai.prices`, then the release table merged tighten-only with the shared table; a shared-only model is floored at its provider's highest release price (add-shared-prices D3). The engine stores priced calls with `Store.AddUsage` and has `Emit` and `Alert` hooks and `GetMeta`/`PutMeta` state. Config's `ai` section is `config.AISection`, resolved to `ResolvedAI`; `aiChanges` (config/aidiff.go) classifies changes, and `Change.Sentence` (config/sentence.go) words them for approvals.

## Goals / Non-Goals

**Goals:** a payer org can keep clients working on a model the guard can't price yet, with the spend still capped and the MSP told; the default stays exactly as today.

**Non-Goals:**
- Watching Bedrock calls the guard ignores today (`InvokeModelWithBidirectionalStream`, async invoke, Rerank). Separate change.
- Per-scope or per-account choice. The allowlists that decide which models are allowed are per org, so this is too.
- Pricing unknown models closer to their real price (by provider, say). Priced models come through shared prices or `ai.prices`.

## Decisions

**D1. One org-wide setting, `ai.unknown_models`: `cutoff` (default) or `count`.** A string rather than a boolean, so a later third behavior (for example, alert only) fits without a rename. Resolved into `ResolvedAI.UnknownModels`, always set (`cutoff` when absent), so the diff compares resolved values. The JSON schema and validation accept exactly the two values.

**D2. `count` prices at the highest known price, input and output separately.** `NewShared` computes `unknownPrice`: the maximum input and the maximum output per 1,000 tokens over the release table, the shared table, and the org's `ai.prices`. Taking each maximum separately means the price used is at least that of any priced model, so an unknown model is never counted as cheaper than a known one. With today's tables that is $0.015 in and $0.075 out (Claude Opus 4.1), which overestimates most new models, possibly by 100 times. Caps trip sooner; that is the intended trade for not cutting off. Rejected: the provider's highest price. It is closer for most models, but a provider's new flagship can cost more than everything it sold before.

**D3. Where the check sits.** In `Evaluate`, the model allowlist check stays before pricing, so an org with `ai.models` set is unaffected for models it doesn't list. For an allowed model with no price: under `cutoff`, the `unknown_model` hit as today. Under `count`, the API-call record of a non-streaming call is priced from its token counts at `unknownPrice`. A streaming call is priced from its completion record as today. A record to be priced with zero input and zero output tokens returns the `unknown_model` hit with detail "no token counts to estimate from". This keeps a model priced per image, second, or search from running uncapped at $0.

**D4. Usage carries the flag.** `activity.Usage` gains `Unpriced bool`, set by the pack for a call counted at `unknownPrice`. Totals and caps treat it like any usage. Hourly `usage_summary` events are unchanged; their totals already include it.

**D5. Report once per model, in the engine.** After a tick's usage is stored, the engine collects the unpriced models it counted and reads a state item `unpriced.<pack>`, a map of model to {first account, first time}. For each model not in it, it adds the entry, emits `unpriced_model` (event id `unpriced_model:<pack>:<model>`, scope `account:<first account>`, detail: model, account, caller, region, the prices used, and the unknown-models setting), and sends one alert: "guardrail: model <model> has no price; counting it at <in>/<out> per 1k. Price it in shared prices or in ai.prices." The map is saved with the reader commit, so a failed tick reports again rather than never. To make "until it is priced" work, each tick drops from the map any model the pack now prices (a `Priced(model) bool` on the pack), so a model priced and later unpriced is reported again. The map is small: one entry per distinct unpriced model.

**D6. Classification and approval wording.** `aiChanges` adds field `ai.unknown_models`: `cutoff` to `count` is loosening, the reverse tightening. `Sentence` words them as "Count calls to models with no price at the highest known price instead of cutting off the account." and "Cut off an account that calls a model with no price, instead of counting the call." The approval title for this alone is "Loosen the AI guard", as for other `ai.` fields.

## Risks / Trade-offs

- Overestimating can trip a cap on light use of a cheap new model. Mitigation: the alert says how to price the model; pricing it stops the overestimate on the next tick.
- An org in `count` mode lets an attacker with leaked credentials use an unpriced model until a cap trips. The API-key tripwire and caps still apply, and the overestimate makes caps trip early. An org that wants zero tolerance keeps `cutoff`.
- A model whose trail records carry token counts in some other unit (a speech or video model reporting tokens) is counted by those counts. It is estimated at the highest text price, which is still likely high.

## Migration Plan

None. Absent means `cutoff`, today's behavior; existing documents and guards are unaffected until an org opts in.
