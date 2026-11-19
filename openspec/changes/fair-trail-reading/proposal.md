# Proposal

## Why

The AI guard reads at most 400 trail files a run, and spends that limit on the account and region prefixes in a fixed alphabetical order. When it's behind (a first run on a busy organization, or a burst in one account), the prefixes early in the order take the whole limit on every run, and the rest wait until that backlog clears. The worst case is the one the guard exists for: a normally idle account that suddenly calls Bedrock heavily is checked only at the 10-minute rediscovery, misses it because the limit is already spent, stays marked idle, and so misses every rediscovery until the backlog is gone. Its calls are counted late, and its cutoff comes late.

## What Changes

- Each run, the guard lists every prefix due for a read (as today), then shares the 400-file limit evenly among the prefixes that have new files, so each one reads part of its new files on every run instead of waiting for the prefixes before it.
- Whatever an even share leaves unused goes to the prefixes that still have files, starting after the prefix where the last run's extra reading stopped, so no prefix is always last.
- A prefix's listing stops once it holds more keys than the run could read, so a large backlog no longer lists a whole day of files on every run.
- The 400-file limit, the 10-minute rediscovery, the 24-hour active window, the 15-minute look-back, and how each file is read once stay the same.

## Capabilities

### New Capabilities

### Modified Capabilities

- `activity-guard`: a backlog in one account or region no longer delays reading another's new files.

## Impact

- `internal/activity/reader.go` (the even share and the saved position), `internal/activity/activitytest` (the fake bucket's listing limit), `internal/awsx/activity.go` (`TrailBucket.List` stops paging at a limit).
- The reader's saved state gains one field. An older guard ignores it, so a rollback is safe.
- No template change. `docs/ai-guard.md` (how the guard catches up when behind); CHANGELOG.
