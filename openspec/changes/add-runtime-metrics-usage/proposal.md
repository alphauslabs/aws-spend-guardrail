# Proposal

## Why

Some Bedrock runtime calls leave no token counts in CloudTrail. A streamed Chat Completions call on `bedrock-runtime` has a call record and a completion record, and neither carries tokens (`docs/aws-facts.md`, 2026-11-24). After add-tier-pricing, the guard trips `unmeasured_call` on such a call unless an org allows it, and an allowed one counts at $0. Either way the spend isn't measured. Bedrock runtime publishes per-model token counts to CloudWatch, split by service tier, and those metrics counted a streamed call's tokens that the trail left out. The guard already reads mantle's metrics through the same cross-account link, so it can measure these calls too.

## What Changes

- In the regions the stack lists for mantle, the guard also reads Bedrock runtime's `AWS/Bedrock` metrics (`InputTokenCount`, `OutputTokenCount`, and `Invocations`) per account, model, and service tier, once a minute.
- For each account, model, and tier, the guard charges the tokens the metrics show beyond what it counted from the trail over the same minutes, at the model's price for that tier. A shortfall is never refunded.
- A call the trail can't measure no longer trips `unmeasured_call` in an account and region whose runtime metrics the guard reads, since the gap pays for it. It still trips where the metrics aren't read (no listed region, or an account not linked), as add-tier-pricing specifies.
- The mantle metric checks extend to runtime metrics: an unreadable region alerts once, and the daily unlinked-account check covers both.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `activity-guard`: the guard reads runtime usage metrics in the listed regions and charges the gap between them and the trail, once per account, model, tier, and window.
- `ai-pack`: a call the trail can't measure is charged from metrics where they're read, and trips `unmeasured_call` only where they aren't.

## Impact

- Depends on add-tier-pricing (tier prices, `unmeasured_call`, and `ai.unmeasured_calls`), which must ship first.
- `internal/activity` (a runtime metrics source and gap reconciliation beside the mantle reader), `internal/awsx` (CloudWatch reads of `AWS/Bedrock`), `internal/packs/ai` (pricing gap records by tier, the `unmeasured_call` exception), and `guard/main.go` (wiring).
- No template change: the `MantleRegions` parameter now also names the regions whose runtime metrics are read, and the guard's role can already read CloudWatch metrics. The parameter keeps its name; the docs say it covers both.
- `docs/ai-guard.md` and `docs/install.md` (what the listed regions cover), and `docs/aws-facts.md` (how runtime metric minutes line up with trail event times).
