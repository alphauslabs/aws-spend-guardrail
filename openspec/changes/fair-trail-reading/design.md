# Design

## Context

See proposal.md (Why). `Reader.ReadNew` in `internal/activity/reader.go` sorts the prefixes, then calls `readPrefix` on each in that order with one shared `budget` of `MaxFilesPerTick` (400). `readPrefix` lists everything after `Newest - Lookback` (`TrailBucket.List` pages to the end), then reads files oldest first until the budget is 0. Prefixes idle for more than `ActiveFor` are skipped except on rediscovery runs. The position (`readerState`) is committed only after the run's usage is stored.

## Goals / Non-Goals

**Goals:**
- Every prefix with new files reads some of them on every run it's polled, while the run's total stays at 400.
- No fixed order: the prefix read last changes from run to run.
- Bounded listing for a prefix with a large backlog.

**Non-Goals:**
- Changing which prefixes are polled when (the 10-minute rediscovery and the 24-hour active window stay). An idle prefix is still first seen only at a rediscovery.
- Raising the 400-file limit or reading files in parallel.
- Prioritizing by spend: the reader doesn't know what a file holds until it reads it.

## Decisions

### D1: List first, then share evenly

Each run splits into a list phase and a read phase. The list phase lists every prefix due this run (the same list requests as today) and keeps, per prefix, its unread keys in order. The read phase gives each prefix with unread keys `share = max(1, budget / n)` files, where `n` is the number of such prefixes, in rotated order (D2). Files left over (from prefixes with fewer than `share` new files, and the remainder of the division) go round-robin, one file at a time, to the prefixes that still have unread keys, in the same rotated order, until the budget or the keys run out.

Alternative: a fixed first-pass share (for example 20 files) and the rest in order. Rejected: with many backlogged prefixes (a first run on a large organization), 20 times the prefix count passes 400, so a prefix late in the rotation still waits several runs. An even share reaches every prefix every run while there are at most 400 of them.

### D2: Rotation from a saved cursor

`readerState` gains `Cursor string`, the prefix after which the next run starts. Both the share pass and the leftover pass walk the sorted prefixes starting after the cursor, wrapping around. At the end of the read phase, the cursor is set to the last prefix that read a file in the run. It is saved with the rest of the position in `Commit`, so a failed usage write repeats the same run's order. An empty or unknown cursor (first run, or a prefix that no longer exists) starts from the top.

When there are more prefixes with new files than the budget, `share` is 1 and the run reaches only the first 400 after the cursor. The next run starts after the last of those, so all are reached within `ceil(n / 400)` runs.

### D3: Bounded listing

`Bucket.List` takes a `limit`: the listing stops paging once it holds at least `limit` keys (S3 returns up to 1,000 per page, so it may return more). The reader passes `budget + len(ps.Seen)`: no run reads more than `budget` files from one prefix, and the seen keys inside the look-back window are skipped. The next run lists again from the prefix's new `Newest - Lookback`, so a cut-off listing loses nothing.

Alternative: keep paging to the end. Rejected: for a prefix days behind, that lists every remaining file on every run, which is slow and adds list requests for files that won't be read for many runs.

### D4: No change to what a file read means

`Newest`, `Seen`, `LastHit`, and the bad-file handling stay as they are. Files within a prefix are still read oldest first, so `Newest` still only moves forward over files actually read.

## Risks / Trade-offs

- [A prefix with a large backlog now catches up more slowly when others also have files] → Intended: total reading is unchanged, and it's shared instead of taken. On a quiet organization one backlogged prefix still gets the whole 400.
- [The list phase holds every due prefix's keys at once] → Bounded by D3 at roughly 1,000 plus 400 keys per prefix, and only for prefixes with new files.
- [An idle account's burst is still seen only at the next rediscovery, up to 10 minutes] → Unchanged by design (see Non-Goals). Before this change it could be much longer during a backlog.

## Migration Plan

Ship in a patch release; no template change. The new `Cursor` field is empty on the first run of the new version, which starts from the top. Rolling back to an older guard ignores the field and reads in the old order, without rereading or skipping files.
