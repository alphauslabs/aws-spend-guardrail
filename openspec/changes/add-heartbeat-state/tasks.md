# Tasks

## 1. Snapshot

- [x] 1.1 Add the snapshot builder (design D1) in `internal/guard`, reading `State`, frozen records, reported unprotected accounts, the last budget reads for actual spend, and, with the AI guard on, the activity store's cutoffs and trail status; tests for a frozen scope, a pending loosening, an excluded and an unprotected account, a cutoff, a stalled trail, and no AI section with the AI guard off
- [x] 1.2 Merge it into the daily heartbeat with `reason: "daily"` and `state_layout`, keeping today's fields; test that the existing heartbeat fields are unchanged

## 2. Change heartbeats

- [x] 2.1 Keep `HeartbeatDigest` in its own state item (not `State`, see design D2) and the digest of the snapshot without spend (design D2); write `heartbeat:<unix>` with `reason: "change"` at the end of `Pull`, `Trail`, `OnThreshold`, and `CheckCoverage` when it differs, with the keys as last checked (design D4)
- [x] 2.2 Tests: a freeze writes one change heartbeat listing it; a lift writes one; a run where only spend moved writes none; applying a version and raising a loosening each write one; a run that changes two things writes one; the first run after upgrade writes one

## 3. Docs and build

- [x] 3.1 Document the heartbeat's contents and "read the newest heartbeat for the guard's state" in `docs/operating.md` (with a `jnh datasets query` example), and add a CHANGELOG entry
- [x] 3.2 Run `gofmt`, `go vet ./...`, and `go test ./...`; verify all pass

## 4. Live check

- [ ] 4.1 On e2e, deploy this build and confirm the first run writes a change heartbeat with the frozen e2estandard03 scope, the 7.20 USD ceiling, the excluded SSO account, and no AI section; then publish a tightening and confirm one more change heartbeat names the new version, and that a run with nothing changed writes none

## Workflow follow-up

- Archive the change, then propose the Jennah console change that reads the newest heartbeat on the payer page.
