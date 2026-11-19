# Tasks

## 1. Guard

- [ ] 1.1 `heartbeatDetail` adds `account_names` after the digest, for the ids the snapshot mentions (D1), from `g.Names`; left out when there are no names
- [ ] 1.2 Tests: names for mentioned accounts only; field absent with no names; a rename alone writes no change heartbeat
- [ ] 1.3 Docs: `docs/operating.md` "The guard's state" describes `account_names`; CHANGELOG `Added`; docs style (no em-dashes, Oxford comma)
- [ ] 1.4 Run `goimports`, `make`, `go vet ./...`, and `go test ./...`; verify all pass

## 2. Console (jennah `app/`)

- [ ] 2.1 `guardState` reads `account_names` defensively; tests, including a heartbeat without it
- [ ] 2.2 The payer page shows `<id> (<name>)` in its scope, excluded, unprotected, AI cutoff, blocked, and mantle unlinked lists, not in the events table (D3); `npm test` and the type check pass
