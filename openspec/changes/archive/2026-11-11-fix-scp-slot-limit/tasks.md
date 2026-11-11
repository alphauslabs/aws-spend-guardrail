# Tasks

## 1. Code

- [x] 1.1 Set `MaxSCPsPerTarget` to 10 in `internal/awsx/preflight.go`, with a comment naming the AWS Organizations quotas page; verify `preflight_test.go` covers a target with 5 SCPs (free), one with 10 (full), and one with 10 including a freeze policy (not full)

## 2. Docs

- [x] 2.1 Say 10 in `docs/operating.md` ("Before installing: check SCP slots") and `docs/install-console.md` (step 2); follow the docs' style (no em-dashes, Oxford comma)
- [x] 2.2 In `docs/aws-facts.md`, mark the SCP limits row confirmed with the current values (10 per target, 10,240 characters) and its source, and add a confirmed row for how long an attached SCP takes to take effect (denied 1.3 to 3.4 seconds after `AttachPolicy` returned, and allowed again as fast after `DetachPolicy`, over four rounds in the e2e organization on 2026-11-11)
- [x] 2.3 Add a `CHANGELOG.md` Unreleased entry under Fixed

## 3. Build

- [x] 3.1 Run `goimports`, `make`, and `go test ./...`; verify all pass
