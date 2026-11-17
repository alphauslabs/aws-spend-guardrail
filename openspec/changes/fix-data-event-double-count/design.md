# Design

## Context

The activity engine hands every parsed trail record to each pack's `Wants`, then `Evaluate`. The AI pack wants `bedrock.amazonaws.com` records whose name is a runtime call or a logging-configuration call. It doesn't look at CloudTrail's `eventCategory`, and nothing dedupes records, so each copy of a call is priced and checked.

Observed in the test organization on 2026-11-17, with a trail logging management events and `AWS::Bedrock::Model`, `AsyncInvoke`, `InlineAgent`, `AgentAlias`, `KnowledgeBase`, and `AWS::BedrockMantle::Project` data events:

- One `Converse` call gave two records: `eventCategory` `Management` (`managementEvent: true`) and `Data` (`managementEvent: false`). They have different `eventID`s and the same `requestID`, `eventTime`, `requestParameters`, and `additionalEventData` (`inputTokens` 3, `outputTokens` 20). The data copy also has a `resources` entry of type `AWS::Bedrock::Model`.
- `InvokeModelWithBidirectionalStream`, `StartAsyncInvoke`, `InvokeInlineAgent`, and `RetrieveAndGenerate` gave one `Data` record each, with no management copy and no token counts.

The guard's preflight already requires the trail to log both read and write management events (`managementProblem` in `internal/awsx/trailcheck.go`), so the management copy of a runtime call is always there.

## Goals / Non-Goals

**Goals:**
- Count, price, and check each runtime call once on a trail that also logs Bedrock data events.
- No new state, no new reads, and no change for trails without data events.

**Non-Goals:**
- Reading calls that are only data events (mantle, bidirectional streams, async invokes, agents, knowledge bases). That is part 2 of the Bedrock blind spots, a separate change.
- Deduping anything outside the AI pack. No other pack reads the trail.

## Decisions

### D1: Skip data-event copies by category, not by request id

The pack ignores a record whose `eventCategory` is `Data`. A record with no `eventCategory` counts as a management event, as CloudTrail's management records always carried one and older test fixtures don't.

Deduping by `requestID` was the alternative. The two copies can land in different trail files, read in different ticks, so it would need a store of recent request ids shared across ticks and Lambda instances, with an expiry. The category check needs none of that and gives the same result, because the management copy is always logged (preflight checks it). It also makes the rule explicit for part 2: data-only calls will be added to `Wants` by name when the pack learns to price them, without reopening this.

### D2: The check lives in the AI pack's `Wants`

`Record` gains `EventCategory`, parsed from `eventCategory`. The engine stays category-blind: a later pack may want data events. `Wants` returns false for `Data` before it looks at the name, so neither pricing nor tripwires see the copy.

### D3: Streaming completion records follow the same rule

A streaming call is priced from its `AwsServiceEvent` completion record. The live check covers `ConverseStream` on a trail logging data events. If the completion record also has a data copy, D1 already skips it; if a streaming call's only completion record were a data event, the check would show it and this design would be revised before release.

## Risks / Trade-offs

- **A trail with data events but no management events** would lose all AI counting. Preflight and the guard's trail check already refuse such a trail, so this is not a new gap.
- **AWS could later log a runtime call only as a data event.** The guard would then stop counting it, silently. The test pins today's records, and `docs/aws-facts.md` records the fact with its date, so a change shows up when the facts are rechecked.
- **Rolling back** to v0.2.3 or earlier brings the double count back on such trails; nothing else breaks.
