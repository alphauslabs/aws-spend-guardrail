## 1. Bounded listing

- [x] 1.1 Add a `limit int` to `activity.Bucket.List`; `TrailBucket.List` (`internal/awsx/activity.go`) stops paging once it holds at least `limit` keys (0 means no limit)
- [x] 1.2 The fake bucket in `internal/activity/activitytest` honors the limit and counts list calls, so tests can check it

## 2. Fair reading

- [x] 2.1 Add `Cursor string` to `readerState`
- [x] 2.2 Split `ReadNew` into a list phase (each due prefix's unread keys, using `budget + len(Seen)` as the list limit) and a read phase
- [x] 2.3 Read phase: an even share per prefix with unread keys, in order rotated after `Cursor`, then the leftover one file at a time round-robin in the same order; keep the per-file handling (`Seen`, `Newest`, `LastHit`, bad files) as it is
- [x] 2.4 Set `Cursor` to the last prefix that read a file; an unknown or empty cursor starts from the top

## 3. Tests

- [x] 3.1 One prefix with 2,000 unread files and an active prefix sorted after it with one new file: the new file is read on the next run
- [x] 3.2 An idle prefix (no file for over 24 hours) gets files while another prefix has a backlog: they're read at the next rediscovery run
- [x] 3.3 40 prefixes with 100 unread files each: about 10 files each per run, every file read exactly once within 10 runs, never more than 400 a run
- [x] 3.4 More prefixes with new files than the limit: every prefix is reached within `ceil(n / 400)` runs, and the order rotates
- [x] 3.5 A failed usage write rereads the same files in the same order on the next run (the cursor isn't committed)
- [x] 3.6 Restart with state written by the previous version (no cursor): reading continues without rereading or skipping
- [x] 3.7 `go vet ./...` and `go test ./...` pass

## 4. Docs and release

- [x] 4.1 `docs/ai-guard.md`: how the guard catches up when it's behind (the run's reading is shared among accounts and regions)
- [x] 4.2 CHANGELOG `[Unreleased]`: the fix, no template change, rollback supported
- [ ] 4.3 Release as a patch and deploy to e2e; check the heartbeat and a pull run
