# Tasks

## 1. Dependencies

- [x] 1.1 Update `jennah-sdk-go` to v0.78.0 or later (`go get`, `go mod tidy`; done as v0.79.0 by add-key-expiry-warning); verify `make` builds and `govulncheck` is clean

## 2. Schema

- [x] 2.1 Add `InboxRetentionDays = 400` and set `Retention` on the `events` declaration in `schema.InboxTables()`, with none on `ConfigTables()` (design D1, D2); verify `schema_test.go` checks the inbox retention's column and days and that the config table has none

## 3. Test fake

- [x] 3.1 Make `jennahtest` keep a declared retention and report it from `GetSchema` (unset keeps, `days = 0` removes), and count a retention change as a declaration that changed something (design D4); verify a fake test for set, keep, and remove

## 4. Setup

- [x] 4.1 Re-declare a table in `Setup` when a column is missing or its retention differs from the declared one; print which tables will have events removed before declaring, and report the response's `retention_change` (design D3); verify CLI tests for an inbox created without retention (retention applied, output names the removal), a second run (no declaration, "nothing changed"), and a new payer org (created with the retention)

## 5. Docs

- [x] 5.1 In `docs/operating.md`, say that inbox events are kept for 400 days (a lower bound, expired events visible for up to about three days), that config versions are kept forever, that running setup again applies the retention to an existing inbox, and how to export events first; follow the docs' style (no em-dashes, Oxford comma)

## 6. Verify

- [x] 6.1 Run `goimports`, `make`, and `go test ./...`; verify all pass
- [x] 6.2 Run the new `guardrail setup` against the e2e payer org's existing inbox (`guardrail.e2e.339712710400.inbox`); confirm the output names the removal, `jnh datasets schema get` shows `RETENTION occurred_at, 400 day(s)`, and a second run reports nothing changed. This verifies that the retention is applied and reported, not that events expire: no e2e event is 400 days old. Expiry on a table that received its retention after creation is verified by Jennah's add-dataset-row-retention live test (6.1), on a 1-day retention
