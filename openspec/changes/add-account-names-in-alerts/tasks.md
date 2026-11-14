# Tasks

## 1. Naming

- [x] 1.1 Add `alerttext.NameAccounts(text string, names map[string]string) string` by design D2; tests: a body and a subject naming one and two accounts, an unknown 12-digit number, ids in an ARN, a cutoff id, an `account:` target, a longer digit run, an id already named (idempotent), an empty name, and the `Error:` line left verbatim
- [x] 1.2 In `awsx.Alerts`, add the optional `Names` source and apply naming to subject and body before the payer prefix, keeping bare ids in the subject when the named one would exceed 100 characters (design D3); tests for both subject cases and for a nil source

## 2. Name source

- [x] 2.1 Add a cached account-name source over `Org.ListAccounts` (design D4): 3-second timeout, refreshed after an hour, keeps the last good list on failure, returns nothing (bare ids) when it has never listed; tests with a fake clock for first use, refresh, a failed refresh, and a failed first listing
- [x] 2.2 Wire it in `guard/main.go` for every alert, including the AI-guard-not-started alert

## 3. Docs and build

- [x] 3.1 Note in `docs/operating.md` (alerts) that alerts name accounts and that events don't; add a CHANGELOG entry; follow the docs' style (no em-dashes, Oxford comma)
- [x] 3.2 Run `goimports`, `make`, `go vet ./...`, and `go test ./...`; verify all pass

## 4. Live check

- [ ] 4.1 On e2e, deploy this build, trigger an AI would-cutoff on 730335604511, and confirm the email's subject and body read `730335604511 (e2enormal)` and its cutoff id is unchanged
