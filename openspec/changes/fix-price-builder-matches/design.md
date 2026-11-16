# Design

## Context

`prices.Build` (internal/prices/build.go) reads each Price List entry, keeps it only when `inferenceType` lowercased is exactly "input tokens" or "output tokens" and its usage type has none of the skipped words (batch, reserved, long_ctx, flex, priority, cache, latency, tpm), and files its per-1,000-token rate under several normalized keys: the `model` and `titanModel` attributes, and the model part of the usage type (`USE1-<model>-input-tokens...`, provider prefix dropped). A Bedrock model is priced when its name or its id key (`idKey`) finds a key with both input and output rates; `pick` takes the highest regional rate, else the highest global one. `TestBuildMatchesTheShippedTable` builds from `testdata/sample.json` and compares with the embedded table.

Observed in the 2026-11-16 Price List (us-east-1):

| Model | Rows | Why missed |
|---|---|---|
| `xai.grok-4.6` | `model` = `xai.grok-4.6`; usage `USE1-xai.grok-4.6-mantle-{input,output}-tokens-standard` ($0.0022, $0.0066) and `-global-standard`; no `inferenceType` | rows skipped (no inferenceType); `norm("xai.grok-4.6")` keeps the provider |
| `mistral.voxtral-small-24b-2507` | `model` = "Voxtral Small 1.0"; usage `USE1-Voxtral-Mini-24B-2507-...` and `USE1-mistral.voxtral-small-24b-2507-mantle-...-standard` ($0.0001, $0.0003) | keys `voxtralmini24b2507` and `voxtralsmall24b2507mantle`, neither equals the id key `voxtralsmall24b2507` |
| `amazon.nova-2-5-sonic` | `model` = "Nova 2.5 Sonic"; `inferenceType` "Speech Understanding input token", "Text Input Token", and output equivalents | rows skipped by the exact inferenceType test |
| `amazon.nova-2-sonic-v1:0` | `model` = "Nova Sonic 2.0", same inference types | rows skipped; key `novasonic2` vs Bedrock's `nova2sonic` |

## Goals / Non-Goals

**Goals:** price these 4 from AWS's own rates, by rules that also catch the same patterns in future models; never lower an existing price.

**Non-Goals:**
- Kimi K3.1 and GLM 5.3: not in the Price List. Manual prices or a research workflow cover them.
- Rerank and Pegasus: priced per search unit and per second of video. Their units are not tokens; the builder already skips them by unit, and stays so.
- A per-model mapping table. Rules over the Price List's shapes, not special cases.

## Decisions

**D1. Direction from `inferenceType` in any wording, else from the usage type.** Lowercased `inferenceType` containing "input token" is input, "output token" is output (covers "Input tokens", "Text Input Token", "Speech Understanding input token"). When `inferenceType` is empty, the usage type decides: `-input-tokens` or `-output-tokens`. An entry with neither is skipped as today. The skip words still apply to the usage type, so batch, flex, priority, and cache rows stay out.

**D2. Keys.** The usage-type model part drops a trailing `-mantle` and modality words (`speech`, `text`) before normalizing; the usage regex also accepts the `-mantle-` and `-<modality>-` forms. A `model` attribute that looks like a model id (contains a dot and no space) is also keyed with `idKey`, so `xai.grok-4.6` yields `grok46`. Existing keys are kept, so every current match still matches.

**D3. A word-order fallback, only when unique.** Each Price List model also gets a bag key: its normalized words, sorted (`Nova Sonic 2.0` gives `2 nova sonic`). A Bedrock model that finds no exact key tries its name's and id's bag keys, and uses one only when exactly one Price List model produced it. An ambiguous bag key matches nothing, so the fallback can't cross-wire two models.

**D4. Several rates per side: the highest.** Speech and text token rates both file under the model's keys; `pick` already takes the highest regional rate per side. For Sonic that is the speech rate, the safe estimate, since the guard can't tell speech tokens from text tokens in the trail.

**D5. No price goes down.** Adding entries and keys only adds rates to a key, and `pick` takes the maximum, so a price can only stay or rise. A test builds the sample with the current code's results recorded first (the shipped table) and fails if any model's price in the new build is lower; the shipped table is then regenerated from the real Price List with `guardrail prices build`, and the diff is reviewed for raises before committing.

## Risks / Trade-offs

- A newly read row could raise an existing model's price (for example, a `-mantle` standard rate above the regular one). That is the tighten-only direction, and the regenerated table's diff shows it for review.
- AWS may change its labels again. The rules cover the shapes seen; the unpriced alert and manual prices remain the backstop.

## Migration Plan

None. The next shared-prices publish (scheduled or by hand) adds the 4 models for every guard; the next release ships them in its table.
