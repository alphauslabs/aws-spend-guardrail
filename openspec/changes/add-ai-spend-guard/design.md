# Design

## Context

- The guard is one Lambda function in the payer (management) account, invoked by named tasks: `pull` every 5 minutes, `daily` at 00:15 UTC, account events, the outbox queue, and Budgets notifications (`guard/main.go`). State is a DynamoDB table; events reach Jennah through an SQS outbox.
- The stack creates freeze policies A and B unattached. The guard's role can detach them, limited to those policy ARNs and to accounts and OUs of the organization; Budgets actions attach them.
- Facts measured in a test organization on 2026-11-11, recorded in `docs/aws-facts.md` by this change:
  - Bedrock runtime calls (`InvokeModel`, `Converse`, and both streaming calls) are free CloudTrail management events. Non-streaming records carry `inputTokens` and `outputTokens` in `additionalEventData`; streaming calls add a completion record (`eventType: AwsServiceEvent`) carrying them. A Bedrock API-key call has `additionalEventData.callWithBearerToken: true`.
  - An organization trail's S3 files hold member calls 0.5 to 5 minutes after the call. CloudTrail events never reach the management account's EventBridge, and a trail can't be narrowed to Bedrock events.
  - An attached SCP applies in 1.3 to 3.4 seconds. Bedrock token quotas can't be lowered.
  - Model ids differ by API: `Converse` records an inference profile id (`us.amazon.nova-micro-v1:0`), `InvokeModel` the profile's ARN.

## Goals / Non-Goals

**Goals:**
- Cut off an account's AI access within minutes of a breach, from the management account alone.
- Make the engine generic, so the compute pack is a pack, not a second guard.

**Non-Goals:**
- Sub-minute cutoffs (they need pieces in member accounts), preventive allowlist SCPs, and the compute pack (each a later change).

## Decisions

### D1. Read the trail by polling S3, every minute
A schedule runs a new `trail` task each minute. It lists new objects under each account and region prefix since its saved position (S3 keys sort by time within a day's prefix) and reads each new file once. Its position per prefix is kept in the state table.

*Alternatives:* S3 event notifications would change the bucket's notification configuration, which a bucket holds only one of and which often belongs to the client's log archive setup. Sending the bucket's events to EventBridge is a bucket setting too. Polling changes nothing on the client's bucket, and adds at most a minute to the 0.5 to 5 minutes CloudTrail takes.

*Scale:* one `ListObjectsV2` per active prefix per minute, and one `GetObject` per new file. Files are small and gzipped. Organizations with many accounts list the day's prefix per region, not per account, once listing per account becomes the larger cost.

### D2. Packs register record matchers
A pack declares the event sources and names it handles (the AI pack: `bedrock.amazonaws.com` runtime calls and their completion records, logging-configuration changes, and `bedrock-agentcore.amazonaws.com` invokes). The reader parses each file once and passes matching records to each pack. A pack returns priced usage, tripwire hits, or nothing. Totals, cutoff, lift, dry-run, events, and alerts are engine code shared by packs.

### D3. Totals in DynamoDB, bucketed by minute
Each priced record adds to a per-account, per-pack counter for its CloudTrail minute (TTL 40 days), and to day and month counters. The hourly total sums the last 60 minute buckets. Using event time, not read time, keeps late files correct. Counter updates are atomic adds, so a retried file is the only double-count risk; D1's once-per-file position prevents it.

### D4. The price table ships with each release
`packs/ai/prices.json` lists, per model, input and output USD per 1,000 tokens, with its source and date. The pack matches a record's model id, inference profile id, or ARN to a model by stripping region prefixes and ARNs to the base model id. Config `ai.prices` overrides per model; a lower override is a loosening, since it lowers estimated spend. A model the table doesn't know trips unless config allowlists it with a price.

### D5. The guard attaches the AI deny policy itself
Unlike the budget pack, no AWS service attaches the policy for it, so the guard's role gains `organizations:AttachPolicy` and `DetachPolicy` on the AI deny policy's ARN and on the organization's accounts, and no other policy. The policy denies `bedrock:InvokeModel`, `bedrock:InvokeModelWithResponseStream`, `bedrock:CallWithBearerToken`, and `bedrock-agentcore:Invoke*` and `bedrock-agentcore:Start*`. It is attached to accounts, never OUs, so one runaway account doesn't cut off its neighbours.

### D6. Cutoffs have ids, and lifts name them
A cutoff id is `ai:<account>:<UTC time, minute>`. The cutoff record holds the reason and evidence. `ai.lifts` in config names ids to lift; adding one is a loosening, so it gets the approval (with the version's reason, from `add-loosening-reason`) and the delay. Raising the cap that was passed also lifts it once applied. Lifts that have been applied stay harmless in config: lifting an id that isn't cut off does nothing.

### D7. Hourly summaries, not raw records
At each hour the guard writes one `ai_usage` event per account and model with calls, tokens, and estimated USD. That is enough for the Jennah agent's baselines and keeps the inbox small: an org with 500 active accounts and 3 models writes about 1,500 summaries an hour at most, usually far fewer. Cutoffs and tripwires are written as they happen.

### D8. Opt-in, and the trail stays the client's
`EnableAIGuard` (default false) creates the AI pieces. `TrailBucket` and optional `TrailPrefix` name the organization trail's bucket. The stack does not create a trail: creating one when the client already has one would bill a second copy at $2 per 100,000 events. If the bucket is in another account (common with a log archive account), the docs give the bucket policy statement granting the guard's role read access.

### D9. Encrypted trail files
Many organization trails encrypt their files with a KMS key (Control Tower's do by default), often a key in the log archive account. `TrailKmsKeyArn` (optional) gives the guard's role `kms:Decrypt` on that key, limited by `kms:ViaService` to S3 in the bucket's region. A key in another account also needs a statement in its key policy allowing the guard's role to decrypt; the docs give it. Without both, reading fails with an access error, which the reader reports as a stalled trail with the cause, and `preflight --ai` catches before enabling.

## Risks / Trade-offs

- [Delay is 0.5 to 5 minutes plus up to 1 minute of polling.] → The worst case is bounded by the burn rate at AWS's default quotas times about 6 minutes. A faster, optional member path is a later change.
- [Estimates are not the bill.] → Prices are list prices per model; CUR and Budgets remain the authority. Summaries let the agent and humans compare.
- [A client's trail is single-region or excludes management events.] → The stack's install check reads the trail's configuration and refuses to enable the AI guard without a multi-region organization trail logging management events.
- [Unknown models trip.] → New models appear often. A release updates the table; until then, a client can allowlist and price a model in config.
- [The client's KMS key policy or bucket policy changes later and the guard loses read access.] → Reads fail closed into the stalled-trail alert, naming the access error, so the gap is visible within 30 minutes.
- [AgentCore runtime compute isn't priced.] → Its model calls through Bedrock are; runtime compute is left to the budget pack. Whether AgentCore invoke calls are management events, and their exact event names, is checked in task 1.1 before the tripwire relies on them.
- [The attach permission is new power.] → Limited to one policy ARN and to accounts; listed in the changelog's Security-relevant section.

## Migration Plan

Existing installs are unaffected. To enable: confirm the organization trail, set `EnableAIGuard=true`, `TrailBucket`, and (for an encrypted trail) `TrailKmsKeyArn` in a stack update, grant read on the bucket and decrypt on the key if they live in another account, then publish an `ai` section in dry-run for a period before `enforce`.

## Open Questions

- Exact AgentCore CloudTrail event names and categories (task 1.1).
- Whether to poll per account or per region prefix by default, decided from the listing cost measured in task 7.2.
