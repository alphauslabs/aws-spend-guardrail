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
- Reading metrics for accounts and models with no unmeasured call. A call kind AWS starts leaving untokenized is still caught, by add-tier-pricing's `unmeasured_call` rule, and once the trail shows it as unmeasured, it is read here too.
- Replacing the trail as the runtime source. The trail keeps the caller, the request, and the tripwires; metrics only fill the gap.
- Refunding a negative gap, or correcting trail-measured calls from metrics.
- A separate list of regions. `MantleRegions` covers both.

## Decisions

### D1: Read the same regions and link as mantle

Runtime metrics are read in the regions `MantleRegions` lists, through the same sink and links, by a second source beside the mantle one. A cross-account link shares an account's metrics, not one namespace, so a linked account already shares `AWS/Bedrock`. The guard's role already has `cloudwatch:GetMetricData`. No template change.

Alternative: a new `RuntimeMetricsRegions` parameter. Rejected: two lists of the same regions, and a template revision, for no case that needs them to differ.

### D2: Targeted reads, per tier

The engine keeps a list of pending gaps: each account, model, region, and 5-minute window in which the trail showed an unmeasured completion. Only those are read. Each `MetricDataQuery` names the account (`AccountId`) and the model, for `InputTokenCount` and `OutputTokenCount`, once per model and once per `ResolvedServiceTier` (the tier that served the call, which is what's billed), with a search expression so tiers that weren't used return nothing. Default-tier tokens are the per-model total minus the tier series. A tier the tables don't price is priced as add-tier-pricing's unknown tier. `Invocations` isn't read: calls are counted from the trail.

Alternative: read every account and model in each listed region every minute, as the mantle reader does. Rejected for cost: about 3 metrics per account and model each read, which at 500 accounts using 5 models each is about $3,000 a month per region at one read a minute, against cents for targeted reads in a typical organization.

### D3: Gap over 5-minute windows, read every 5 minutes once settled

The trail and the metrics don't place a call in the same minute: the trail uses the call's start, and a long stream's metrics may land in a later minute. So the gap is taken over 5-minute windows, not minutes: the engine records the tokens it counted from the trail per account, model, tier, and window (by `eventTime`), and the reconciler compares them with the metrics' sum over the same window. The reconciler runs every 5 minutes, on the guard's existing one-minute run when the clock crosses a 5-minute mark. A pending window is read when it is 10 minutes old, after its trail records have arrived (at most 5 minutes late) and its metrics have settled, and once more at 15 minutes for growth; then it is dropped. A negative gap charges nothing and isn't carried forward.

A streamed call is charged 10 to 15 minutes after it starts, not within a minute. Its account's other calls are counted from the trail as they arrive, so a cap is still checked within minutes; only the streamed call's own tokens wait.

Task 1.1 measures how a long streamed call's metrics line up with its `eventTime`. If they can land more than a window apart, the window widens; the specs don't change.

### D4: A gap is a record the AI pack prices

The reconciler emits a usage record per account, model, tier, and window with a gap, with `EventCategory` `Metric`, the tier set, and no caller. The AI pack prices it at the tier's price and adds it to the account's totals with zero calls, since the unmeasured call itself already counted as one (D5). Allowlists, region lists, and the unknown-model policy already applied to the call record.

### D5: The unmeasured-call exception

The AI pack is told which accounts and regions have readable runtime metrics: listed regions, with the account linked and the last read successful. An unmeasured completion there counts as one call at $0 and doesn't trip. Elsewhere add-tier-pricing's D9 applies. A failed read makes the region "not readable" until reading works, so an outage falls back to tripping rather than letting calls through unmeasured.

## Risks / Trade-offs

- [The metrics count tokens the trail prices differently, such as prompt-cache tokens, and the gap charges them.] → The charge errs high, the safe direction. Task 1.2 compares the two for a cached call and records it; if they differ systematically, the reconciler subtracts the known difference.
- [A call charged from the trail at a different tier than the metrics' `ResolvedServiceTier` (asked Priority, served standard) shows a gap in one tier and a surplus in another.] → The gap is charged and the surplus isn't refunded, so the call can be charged twice. Rare; task 1.1 checks whether a downgrade is visible in the trail and, if it is, the trail side uses the served tier.
- [A streamed call's tokens count 10 to 15 minutes late, so an account streaming fast can pass a cap by that much before the guard trips.] → The same lag the mantle reader accepts at its 15-minute reach; the docs say so. An org that can't accept it keeps `ai.unmeasured_calls` at `cutoff` in regions it doesn't list.
- [CloudWatch reads cost money.] → Only accounts and models with unmeasured calls are read, every 5 minutes, about two metrics per tier used: an organization with no streamed Chat Completions calls reads nothing. `docs/install.md` gives the per-call-pattern estimate.
- [An outage of the metrics read makes unmeasured calls trip again.] → Intended (D5): block first while blind. The region's alert says why.

## Migration Plan

1. Ship after add-tier-pricing. Stacks with `MantleRegions` set start reading runtime metrics on upgrade; stacks without it see no change.
2. Orgs that set `ai.unmeasured_calls` to `allow` because their accounts stream can keep it or move back to `cutoff`: with the metrics read, those calls are charged either way.
3. Rollback: the previous release ignores runtime metrics and trips or allows unmeasured calls as configured.
