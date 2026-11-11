# Tasks

## 1. Guard

- [x] 1.1 Keep the version's note with a pending loosening (`Pending.Note`), set when the approval is raised
- [x] 1.2 In `approvalRequest`, add a "Reason given" part to the summary (the note, shortened past 4,000 characters with a line naming the version, or "No reason was given."), and a `reason` field in the payload; record the note in the `loosening_raised` event; verify pull tests for a note, no note, and a 10,000-character note (summary under Jennah's 16,000-character limit, approval created)

## 2. Docs

- [x] 2.1 In `docs/config.md` (Tightening and loosening) and `docs/operating.md` (pull, edit, publish), say that the `--note` of a loosening version is shown to approvers as the reason, so give one; follow the docs' style (no em-dashes, Oxford comma)
- [x] 2.2 Add a `CHANGELOG.md` Unreleased entry under Added

## 3. Build

- [x] 3.1 Run `goimports`, `make`, and `go test ./...`; verify all pass
