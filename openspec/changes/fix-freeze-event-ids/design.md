# Design

## Context

`recordFreeze` (`internal/guard/freeze.go`) builds the freeze id as `freeze:<target>:<period start>` and emits it through the SQS outbox, whose consumer commits with the id as the idempotency key. `lift` uses `lift:<target>:<Frozen.Since unix>`. `OnThreshold` returns early, writing nothing, when the scope is already in the frozen records. `fix-budget-spend-reset` added that check so the Budgets action firing after a guard attach isn't a second freeze.

## Goals / Non-Goals

**Goals:**
- One `freeze` event per freeze, including repeated freezes in a period.
- Repeats of the same freeze produce no new event and no new alert.

**Non-Goals:**
- Backfilling freezes already lost.
- Changing `would_freeze`, `lift`, or other event ids.

## Decisions

### D1. Key the id on the freeze's `Since`
`freeze:<target>:<period start>:<Since unix seconds>`, with `Since` being the time stored in the frozen record. It is fixed when the freeze is recorded, survives restarts, and pairs the event with its `lift` (same `Since`). Keeping the period in the id leaves ids readable and sorted by period.

*Alternative:* a counter per period. Rejected, because it needs another stored value and still has to be read back on a repeat.

### D2. A repeat re-emits with the recorded `Since`, without alerting
When `OnThreshold` finds the scope already frozen, it emits the freeze event built from the stored `Frozen` record (same id) and returns without alerting. If the first emit reached the outbox, the duplicate is dropped downstream. If it never did (the run failed between `MarkFrozen` and the emit), this writes it. The watch path (`watchSpend`) skips frozen scopes before reading spend, as now.

The repeat's detail is built from the current notification. When it is dropped as a duplicate, the first detail stands. When it fills a lost event, `how` says `budget_action` even if the guard attached the policy first. That is acceptable for a recovery path.

## Risks / Trade-offs

- [Ids change format for new freezes] → Readers treat ids as opaque. The console derives state from `kind`, `scope`, and `occurred_at`, not the id. Existing rows keep their ids.
- [Two events if `Since` were rewritten for the same freeze] → `MarkFrozen` is only called when the scope isn't already recorded as frozen, so `Since` doesn't change for a freeze in force.
