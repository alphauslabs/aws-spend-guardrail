# Tasks

## 1. Reach and reads

- [ ] 1.1 In `schema/schema.go`, add `KindGuardInstalled = "guard_installed"`, and add the payer org's inbox to the reader credential's datasets with read only; update `schema_test.go` so the reader reaches config, prices, and inbox and still has no write permission, and the writer is unchanged
- [ ] 1.2 Add `ConfigSource` reads for the latest event of a kind in the inbox and a config version by number (the client already has `LatestEvent` and `GetVersion`); add them to the fakes in `internal/guard`
- [ ] 1.3 Make `guardrail setup` mint the reader with the new reach, and verify in `internal/cli` tests that an existing reader key is reported as needing rotation rather than silently kept

## 2. First run

- [ ] 2.1 Add `InstallID` and `InstallChecked` to `State`; set `InstallID` on the first run of a guard with no applied version and no install ID
- [ ] 2.2 Implement the install check (design D1, D3, D5): read the last `config_applied`, read and apply that version, then let `evaluate` handle the latest; on a permission or read error, alert once and apply the latest; write `guard_installed` once with its outcome; retry on the next run if Jennah is unreachable
- [ ] 2.3 Scope version event IDs by install (design D4): `<kind>:<N>:i<InstallID>` when `InstallID` is set, `<kind>:<N>` otherwise
- [ ] 2.4 Tests in `internal/guard`: reinstall with a pending loosening puts the old version in force and asks for approval; the existing approval is reused (one request key, one approval); reinstall after a rejected loosening keeps the old version; reinstall with nothing pending applies at once; first install applies at once; no inbox reach alerts once and applies the latest; a restart with state writes no `guard_installed`; an upgraded state without `InstallID` keeps the old event IDs

## 3. Docs and build

- [ ] 3.1 Document reinstalling a guard in `docs/operating.md` (what carries over, what the new guard does with a pending loosening, the `guard_installed` event, and the inbox trust in design's first risk), note it under deleting the stack in `docs/install.md`, update the reader key's reach where keys are described, and add a CHANGELOG entry with a Security-relevant line for the reader's inbox read
- [ ] 3.2 Run `gofmt`, `go vet ./...`, and `go test ./...`; verify all pass

## 4. Live check

- [ ] 4.1 On e2e, remint the reader key with the inbox, publish a loosening, and before approving it delete and recreate the stack; confirm the old version is in force, no second approval email arrives, `guard_installed` says `resumed`, and approving the existing request applies the version after the delay

## Workflow follow-up

- Deploy the guard to e2e, then archive the change.
