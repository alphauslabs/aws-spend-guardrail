# Design

## Context

See proposal.md for why. Today:

- `Guard.Daily` (internal/guard/freeze.go) writes `heartbeat:<YYYY-MM-DD>` with `applied_version`, `effective_version`, `pending`, the keys' expiry, and `selfReport` (guard version, code hash). A second daily run on the same day is dropped as a duplicate id, which is fine for a once-a-day event.
- The state the snapshot needs is all in the payer account: `State` (effective config, applied and effective versions, pending loosening), frozen records (`State.ListFrozen`), reported accounts (`State.Reported` for `unprotected_account`), the activity store's cutoffs (`ActivityStore.Cutoffs`) and its trail status (last file, stalled cause).
- Entry points: `Pull` (every 5 minutes), `Trail` (every minute, AI guard only), `OnThreshold` (budget notifications), `CheckCoverage` (account events), `Daily`.

## Goals / Non-Goals

**Goals:** any reader gets the guard's state from the newest heartbeat; the heartbeat is never staler than the last change.

**Non-Goals:**
- Live spend. Budgets' actual spend is included as last read, and Cost Explorer is not called for the snapshot.
- A history API. Older heartbeats are the history, kept for 400 days like every event.
- Changing the console here. Its payer page reads the snapshot in a follow-up Jennah change.

## Decisions

**D1. One snapshot builder, used by every heartbeat.** `snapshot(ctx, st)` returns a struct with `state_layout` (1), `scopes` (target, mode, ceiling, threshold, period, freeze style, `actual_usd` from the last budget read when known, and `frozen` with `since`, `how`, `policy_id` for frozen ones), `pending` (version, approval id, `first_seen`, `earliest_apply_at`), `excluded`, `unprotected` (account and first reported), and `ai` when the AI guard is on (mode, `cutoffs` with account, rule, since, attached, and `trail` with last file and stalled cause). It is merged into the existing heartbeat detail, so today's fields keep their names and readers of them are unaffected.

**D2. A digest decides whether state changed.** The digest is a hash of the snapshot with spend figures removed, kept in its own state item (`HeartbeatDigest` and `SetHeartbeatDigest` on the state store), not in `State`: `Trail` and `OnThreshold` never save `State`, and saving it there could overwrite a concurrent `Pull`'s. `Pull`, `Trail`, `OnThreshold`, and `CheckCoverage` compute it at the end of a run; when it differs from the stored one, they write a heartbeat with id `heartbeat:<unix seconds>:<first 12 hex of the digest>` (so two states written in the same second stay apart, and a retried write of the same state stays idempotent) and `reason: "change"`, then store the digest. `Daily` always writes `heartbeat:<YYYY-MM-DD>` with `reason: "daily"`, the keys, and the snapshot, and stores the digest. Alternative: emit next to each event that changes state (`freeze`, `lift`, and the rest); rejected because it spreads the rule over a dozen call sites, and a run that changes several things would write several heartbeats.

**D3. Concurrent runs may both write.** `Trail` and `Pull` can run at once and both see a changed digest. Both heartbeats are correct snapshots, and the newer one wins for a reader, so this is tolerated rather than locked.

**D4. Keys are carried as last checked.** The keys' expiry is read once a day. A change heartbeat includes the keys as recorded in `State.KeyWarnings` from the last daily check, so the newest heartbeat always has them.

## Risks / Trade-offs

- [The `Trail` run, every minute, now builds a snapshot.] → It reads state the run already holds plus the frozen and reported records, a few DynamoDB reads; the heartbeat is written only on change.
- [A large org's heartbeat is bigger.] → About 150 bytes a scope, so about 15 KB at 100 enforcing scopes plus dry-run scopes; well within a JSON column.
- [A reader that wants liveness must not mistake a change heartbeat for a daily one.] → `reason` says which; liveness is "a heartbeat within 26 hours", which either kind satisfies.

## Migration Plan

Ship in a guard release. The first run after the upgrade has no stored digest, so it writes one change heartbeat with the full state. Rollback is the previous release, which ignores `HeartbeatDigest`.
