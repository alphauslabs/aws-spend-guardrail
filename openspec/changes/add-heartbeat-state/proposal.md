# Proposal

## Why

A person or a model asking "what does the guard protect right now, and what is frozen?" has to rebuild the answer from the inbox's events: the newest `freeze` per scope without a later `lift`, cutoffs without a `cutoff_lifted`, the newest `trail_stalled` against `trail_resumed`, and `loosening_raised` without a close. The Jennah console does that in TypeScript (`app/src/lib/guardrail.ts`) over a bounded window of recent events, so a freeze older than the window can drop out of view. Any other reader (a CLI, an MCP tool, a model in an AI harness) would have to repeat the same derivation, or get it wrong.

The guard already knows this state exactly, from its own state table and AWS, and it already writes a daily `heartbeat` event. Today the heartbeat carries only the applied and effective versions, whether a loosening is pending, the guard version and code hash, and the keys' expiry. It is the natural place for the guard to publish its state, so every reader gets it from one row.

## What Changes

- The heartbeat's detail gains the guard's current state: every scope in force with its ceiling, threshold, mode, period, freeze style, and Budgets' actual spend; which scopes are frozen, since when, and how; the pending loosening with its approval id and earliest apply time; excluded and unprotected accounts; and, with the AI guard on, its mode, the accounts cut off, and whether the trail is stalled. A `state_layout` number says which shape it is.
- The guard also writes a heartbeat when that state changes (a freeze or lift, a cutoff or its lift, a version applied or a loosening raised or closed, a trail stall or resume, a change in which accounts are unprotected), at most once per run, with an id that includes the time. The daily heartbeat stays, with its existing id and its key expiry. A reader takes the newest heartbeat.
- Spend figures don't count as a change, so a scope's spend moving doesn't write a heartbeat; they are as of the snapshot.
- No change to the inbox's tables, the keys, or the stack.

## Capabilities

### New Capabilities

### Modified Capabilities

- `guard-jennah-contract`: the heartbeat carries the guard's state, and is written when that state changes as well as daily.

## Impact

- `internal/guard`: a snapshot builder (from `State`, frozen records, reported accounts, the activity store's cutoffs and trail status), a digest of its non-volatile parts kept in `State`, and an emit at the end of `Pull`, `Trail`, `OnThreshold`, and `CheckCoverage` when the digest changes; `Daily` writes the full heartbeat as now, with the state added.
- Docs: `docs/operating.md` (what the heartbeat holds and how to read the newest one), CHANGELOG.
- Inbox volume: one heartbeat a day plus one per state change, a handful on a busy day. Each is a few KB; about 15 KB with 100 scopes.
- Follow-up in Jennah: the console's payer page reads the newest heartbeat instead of rebuilding state from events, proposed separately once a guard with this change is deployed.
