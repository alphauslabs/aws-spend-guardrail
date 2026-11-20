# Proposal

## Why

On e2e, inbox events arrived hours late or not at all, while the guard itself worked. `config_applied` for a version the guard applied at 01:32 reached the inbox about 3 hours later, so `guardrail pull` kept reporting the version as pending, and its diff for the next version compared against the wrong base. Two causes:

- The outbox consumer runs in the guard's function, which has reserved concurrency 1 so guard tasks never overlap. Outbox batches compete with the pull, trail, and daily runs for that one slot and are throttled: 12 to 29 throttles every half hour on e2e, even with the AI guard off. A throttled batch waits out the queue's 5-minute visibility timeout; one event was received 37 times before it was written.
- An event whose id is already in the inbox with different content is refused for good ("idempotency key already used for a different commit"). The daily heartbeat's id is `heartbeat:<date>`, so a second daily run on the same day (a manual run, or a Lambda retry of the daily task) queues an event Jennah will never take. The consumer retries it every 5 minutes for about 3.5 days, then the dead-letter alarm fires. Two such heartbeats from 2026-11-18 had been retried 467 times. Any re-emitted id with a new time has the same problem.

The consumer also doesn't log why a write failed, which is why this went unseen.

## What Changes

- The outbox consumer moves to its own small function, from the same package, with its own role (the outbox queue, the writer and reader secrets, and its log group) and up to 2 concurrent invocations. The guard function keeps reserved concurrency 1 for its tasks, which the consumer never needed: it only writes to Jennah.
- When a write is refused as invalid, the consumer checks the inbox for the event's id with the reader key. If the id is there, the event counts as delivered (the first write stands) and is removed from the queue. Otherwise it's retried as before.
- Every failed write is logged with the event id and the error.
- The daily heartbeat's id becomes `heartbeat:<date>:<digest>`, from its content, so a second daily run on the same day writes its own heartbeat, and a retried write of the same one is still deduplicated.
- Template revision 8.

## Capabilities

### New Capabilities

### Modified Capabilities

- `guard-jennah-contract`: events reach the inbox without waiting on guard tasks; a repeated event id counts as delivered; failed writes are logged; a second daily heartbeat on a day is its own event.

## Impact

- `guard/template.yaml`: `OutboxFunction`, `OutboxRole`, its log group, the event source mapping moved to it with a maximum concurrency of 2, revision 8.
- `guard/main.go`: an outbox-only mode that reads just the two keys it needs. `internal/outbox`: the inbox check and logging. `internal/guard/freeze.go`: the daily heartbeat id. `internal/guard/deployment.go`: `MinTemplateRevision` 8.
- `docs/operating.md` (the daily heartbeat id), `docs/install.md` or `docs/upgrade.md` if they list the stack's functions; CHANGELOG.
- Rollback to v0.2.11 needs the revision 7 template back, since that release's function expects to consume the queue.
