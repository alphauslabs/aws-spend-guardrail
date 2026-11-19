# Tasks

## 1. Guard

- [ ] 1.1 `aiSnapshot` adds `unknown_models`, `blocked` (from state meta `ai.blocked`, only with a block policy), and `mantle` (watched regions with read status from `mantle.status`, and unlinked accounts from the `mantle_unlinked` reports, only with watched regions), as design D1; all in the digest (D2)
- [ ] 1.2 Tests: the fields appear and are absent as D1 says; a change in blocks or a region's readability writes a change heartbeat; an unchanged state writes none
- [ ] 1.3 Docs: `docs/operating.md` "The guard's state" lists the new fields and the new change triggers; CHANGELOG `Added` (no upgrade beyond updating the stack's package); docs style (no em-dashes, Oxford comma)
- [ ] 1.4 Run `goimports`, `make`, `go vet ./...`, and `go test ./...`; verify all pass

## 2. Console (jennah `app/`)

- [ ] 2.1 `guardState` reads `ai.unknown_models`, `ai.blocked`, and `ai.mantle` defensively; tests, including a heartbeat without them
- [ ] 2.2 The payer page's AI section shows blocked kinds per account (noting a policy not attached or failing), each mantle region's status and unlinked accounts, and the unknown-model policy (D3); `npm test` and the type check pass
