# Design

## Context

See proposal.md (Why). The stack has one function, `GuardFunction` (`guard/main.go`), that dispatches on its input: schedules (pull every 5 minutes, trail every minute with the AI guard on, daily, price publishing), organization events, the budget notification topic, and the outbox queue (`OutboxEventSource`, batch size 10, `ReportBatchItemFailures`). It has `ReservedConcurrentExecutions: 1` so tasks never run against the same state at once. The outbox queue has a 5-minute visibility timeout and `maxReceiveCount` 1000, then a dead-letter queue with an alarm. `outbox.Consumer.Handle` writes each event with `jennah.Client.CommitEvent` (an insert with idempotency key `event:<id>`, treating `AlreadyExists` as success) and reports the rest as batch item failures, without logging. The writer key has `datastore.data:write` on the inbox only; the reader key can read the inbox.

Observed on e2e (2026-11-20): Lambda reported no errors but 12 to 29 throttles per half hour; a valid `config_applied` event was received 37 times before it was written; replaying stuck valid events with the same client from outside succeeded at once; two `heartbeat:2026-11-18` events had 467 receives and failed with `InvalidArgument: idempotency key already used for a different commit`.

## Goals / Non-Goals

**Goals:**
- Queued events are written within minutes, independent of guard task load.
- An event Jennah already holds under the same id never clogs the queue.
- A failed write is visible in the logs.

**Non-Goals:**
- Changing the guard's own one-at-a-time task rule.
- Changing which events are emitted, their ids (other than the daily heartbeat), or their content.
- Faster dead-lettering of events that are invalid for other reasons: they keep the existing retry and alarm, now with a log line per attempt.

## Decisions

### D1: A separate outbox function from the same package

`OutboxFunction` uses the same `FunctionPackageBucket`/`FunctionPackageKey` and handler, with `OUTBOX_ONLY=true` in its environment. In that mode `main` reads only the writer and reader secrets and builds only the consumer; every other input is refused with an error. It has its own `OutboxRole`: `sqs:ReceiveMessage`, `DeleteMessage`, `GetQueueAttributes` on the outbox queue, `secretsmanager:GetSecretValue` on the two key secrets, and logs to its own log group `/aws/lambda/guardrail-${MSPId}-outbox`. No reserved concurrency; the event source mapping sets `ScalingConfig.MaximumConcurrency: 2`, so it never takes more than two of the account's unreserved slots. `OutboxEventSource` points at the new function; `GuardRole` loses its SQS receive and delete permissions (it keeps `SendMessage`).

Alternatives: raise `GuardFunction`'s reserved concurrency. Rejected: the one-at-a-time rule protects state the tasks share. A separate package for the consumer. Rejected: a second build artifact to sign and verify for a few lines of code.

### D2: Check the inbox on an invalid write

On a write error whose gRPC code is `InvalidArgument`, the consumer reads the inbox for a row with that `event_id` with the reader key. Found: the message is delivered (not reported as a failure), and a log line says the inbox already held the id. Not found, or the read fails: reported as a failure, as today. Other errors are retried without the lookup.

Alternative: match the error message text ("idempotency key already used"). Rejected: the text isn't a contract, and the lookup answers the question that matters, whether the inbox has the event.

### D3: The daily heartbeat id carries a digest

`heartbeat:<date>:<12 hex of SHA-256 of the detail>`, like the change heartbeat's `heartbeat:<unix>:<digest>`. A retried emit of the same heartbeat has the same detail and so the same id. Docs change from `heartbeat:<date>`; nothing reads the id's form (the console and CLI find heartbeats by kind and time).

### D4: Log lines

`outbox: write <id> (<kind>) failed: <error>` and `outbox: <id> (<kind>) already in the inbox; delivered`. No detail, which can name accounts and prices.

## Risks / Trade-offs

- [Two outbox invocations can write events out of order] → Events carry their own `occurred_at`, and readers order by it; order of arrival was never guaranteed (retries already reorder).
- [The outbox function reads the reader key too] → Read-only on the guard's datasets; the guard function already holds both.
- [A cold start reads two secrets] → Once per instance, as today.
- [Rollback] → v0.2.11's package can't run in outbox-only mode, so rolling back means updating the stack with v0.2.11's template (revision 7), which moves the queue back to the guard function. The Upgrade section says so.

## Migration Plan

Template revision 8; `MinTemplateRevision` 8. An upgrade updates the stack with the release's template and package. Messages already queued stay queued and are delivered by the new function; a queued duplicate daily heartbeat from an earlier release leaves the queue through D2. Rollback: update the stack with the previous release's template and package.
