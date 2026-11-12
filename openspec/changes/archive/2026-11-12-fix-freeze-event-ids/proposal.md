## Why

A scope frozen, lifted, and frozen again in the same budget period loses its second `freeze` event. The id is `freeze:<scope>:<period start>`, the same for every freeze in a period, and Jennah keeps one row per event id. So the outbox commits the second freeze as a duplicate of the first, and the inbox shows the scope as lifted while its freeze policy is attached.

The e2e live check of `fix-budget-spend-reset` hit this. e2estandard03 was frozen at 20:17, lifted at 21:15 after an approved raise, and frozen again by the guard at 21:40. The policy was attached and the local alert went out, but no `freeze` event reached the inbox. The console's installed view, which works out active freezes from the inbox, would report no freeze.

`add-budget-freeze-guard` design D8 called for `freeze:<scope>:<period>:<action execution time>`. The implementation dropped the time part.

## What Changes

- A `freeze` event's id includes the freeze's start time: `freeze:<scope>:<period start>:<unix seconds>`. That's the `Since` the guard records for the freeze, and it's already what the matching `lift` event's id uses.
- A repeat of the same freeze, such as a retried or duplicate budget notification, or the Budgets action firing on a scope the guard already froze, re-emits the event with the recorded `Since`. The outbox and Jennah drop it as a duplicate, and no second alert goes out. A freeze whose event was lost before reaching the outbox is written on the repeat.
- `would_freeze` keeps one event per scope per period, since a dry-run scope is never lifted.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `guard-jennah-contract`: each freeze of a scope is its own inbox event, while a repeat of the same freeze is not.

## Impact

- `internal/guard/freeze.go`: the freeze id and the already-frozen path in `OnThreshold`.
- Tests in `internal/guard`.
- `CHANGELOG.md` (Fixed). No stack, IAM, or schema change.
- Existing events keep their ids. The e2e inbox's missing 21:40 freeze isn't backfilled; the next run that sees that freeze (for example, the Budgets action firing once Budgets has spend) writes it.
