# Proposal

## Why

When an organization trail also logs Bedrock model data events (`AWS::Bedrock::Model`), CloudTrail records each `Converse` call twice: once as a management event and once as a data event, with different event ids, the same request id, and the same token counts (seen in the test organization on 2026-11-17). The AI pack counts both, so every estimate on such a trail is doubled and accounts are cut off at half their caps. A payer org may log these data events for reasons of its own, so released guards can be wrong today without anyone changing the guard's setup.

## What Changes

- The AI pack counts each Bedrock runtime call once: it ignores the data-event copy of a call that CloudTrail also logs as a management event (`InvokeModel`, `InvokeModelWithResponseStream`, `Converse`, `ConverseStream`, their completion records, and the logging-configuration calls). It already requires the trail to log management events, so the management copy is always there.
- Tripwires fire once per call on such a trail, not twice.
- Data events the AI pack doesn't read yet (bidirectional streams, async invokes, mantle, agents, knowledge bases) are still ignored, as today; reading them is a separate change.
- A test with records from the test organization's trail, both copies of one call, pins the behavior.
- `docs/ai-guard.md` says the guard counts each call once whether or not the trail logs Bedrock data events.

## Capabilities

### New Capabilities

### Modified Capabilities

- `ai-pack`: a call CloudTrail records as both a management and a data event is priced and checked once.

## Impact

- `internal/activity/record.go`: the record keeps CloudTrail's `eventCategory`.
- `internal/packs/ai/ai.go`: `Wants` skips data-event copies of management-logged calls.
- Tests in `internal/activity` and `internal/packs/ai`, with a trail file built from the captured records.
- `docs/ai-guard.md`, `docs/aws-facts.md` (the double logging, confirmed), and `CHANGELOG.md` (a fix, no stack update needed beyond the new package).
- No template, config, or state change. Rolling back restores the double count on such trails.
