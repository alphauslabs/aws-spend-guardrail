# Design

## Context

See proposal.md for why. The current state, after add-tier-pricing:

- `internal/activity/metrics.go` reads mantle usage per account, model, and minute from `AWS/BedrockMantle` through the payer account's CloudWatch cross-account observability sink, in each region listed in `MantleRegions`. It revisits the last 15 minutes (`MantleWindow`), counts only growth, and keeps what it counted in the state table until usage is stored.
- The trail reader prices runtime calls from CloudTrail records, which land 0.5 to 5 minutes after the call (`docs/aws-facts.md`). Records carry the call's start time as `eventTime`.
- Observed on 2026-11-24 in the e2e payer account (`docs/aws-facts.md`): `AWS/Bedrock` publishes `Invocations`, `InputTokenCount`, and `OutputTokenCount` per `ModelId`, and for tiered calls also per `ServiceTier` and `ResolvedServiceTier`. A default-tier call adds to the per-model series only. The metrics counted a streamed Chat Completions call's tokens, which the trail left out.
- add-tier-pricing trips `unmeasured_call` on a streamed completion with no token counts unless `ai.unmeasured_calls` is `allow`.

## Goals / Non-Goals

**Goals:**
- Charge runtime tokens the trail can't measure, by tier, without charging trail-measured calls twice.
- Stop tripping `unmeasured_call` where the gap covers the call.

**Non-Goals:**
- Replacing the trail as the runtime source. The trail keeps the caller, the request, and the tripwires; metrics only fill the gap.
- Refunding a negative gap, or correcting trail-measured calls from metrics.
- A separate list of regions. `MantleRegions` covers both.

## Decisions

### D1: Read the same regions and link as mantle

Runtime metrics are read in the regions `MantleRegions` lists, through the same sink and links, by a second source beside the mantle one. A cross-account link shares an account's metrics, not one namespace, so a linked account already shares `AWS/Bedrock`. The guard's role already has `cloudwatch:GetMetricData`. No template change.

Alternative: a new `RuntimeMetricsRegions` parameter. Rejected: two lists of the same regions, and a template revision, for no case that needs them to differ.

### D2: Per tier, from three series

For each account and model, the source reads the per-model series and the per-`ResolvedServiceTier` series (the tier that served the call, which is what's billed). Default-tier tokens are the per-model total minus the tier series. A tier the tables don't price is priced as add-tier-pricing's unknown tier.

### D3: Gap over 5-minute windows, charged once a window has settled

The trail and the metrics don't place a call in the same minute: the trail uses the call's start, and a long stream's metrics may land in a later minute. So the gap is taken over 5-minute windows, not minutes: the engine records the tokens it counted from the trail per account, model, tier, and window (by `eventTime`), and the reconciler compares them with the metrics' sum over the same window. A window is charged once it is 10 minutes old, when its trail records have arrived (at most 5 minutes late) and its metrics have settled. It is revisited for growth until the 15-minute window passes. A negative gap charges nothing and isn't carried forward.

Task 1.1 measures how a long streamed call's metrics line up with its `eventTime`. If they can land more than a window apart, the window widens; the specs don't change.

### D4: A gap is a record the AI pack prices

The reconciler emits a usage record per account, model, tier, and window with a gap, with `EventCategory` `Metric`, the tier set, and no caller. The AI pack prices it at the tier's price and adds it to the account's totals with zero calls, since the unmeasured call itself already counted as one (D5). Allowlists, region lists, and the unknown-model policy already applied to the call record.

### D5: The unmeasured-call exception

The AI pack is told which accounts and regions have readable runtime metrics: listed regions, with the account linked and the last read successful. An unmeasured completion there counts as one call at $0 and doesn't trip. Elsewhere add-tier-pricing's D9 applies. A failed read makes the region "not readable" until reading works, so an outage falls back to tripping rather than letting calls through unmeasured.

## Risks / Trade-offs

- [The metrics count tokens the trail prices differently, such as prompt-cache tokens, and the gap charges them.] → The charge errs high, the safe direction. Task 1.2 compares the two for a cached call and records it; if they differ systematically, the reconciler subtracts the known difference.
- [A call charged from the trail at a different tier than the metrics' `ResolvedServiceTier` (asked Priority, served standard) shows a gap in one tier and a surplus in another.] → The gap is charged and the surplus isn't refunded, so the call can be charged twice. Rare; task 1.1 checks whether a downgrade is visible in the trail and, if it is, the trail side uses the served tier.
- [CloudWatch reads cost money: three series per account, model, and region each minute.] → `GetMetricData` with search expressions, as for mantle, batched per region. The operating docs give the estimate.
- [An outage of the metrics read makes unmeasured calls trip again.] → Intended (D5): block first while blind. The region's alert says why.

## Migration Plan

1. Ship after add-tier-pricing. Stacks with `MantleRegions` set start reading runtime metrics on upgrade; stacks without it see no change.
2. Orgs that set `ai.unmeasured_calls` to `allow` because their accounts stream can keep it or move back to `cutoff`: with the metrics read, those calls are charged either way.
3. Rollback: the previous release ignores runtime metrics and trips or allows unmeasured calls as configured.
