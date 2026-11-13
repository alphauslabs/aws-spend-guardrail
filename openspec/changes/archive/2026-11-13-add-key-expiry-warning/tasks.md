# Tasks

## 1. Jennah client

- [x] 1.1 Once Jennah `add-whoami-api-key` is released, update `jennah-sdk-go` and add a `WhoAmI` call in `internal/jennah` returning the calling key's id, label, and expiry, or unknown when the response has no key (design D1, D3); verify tests against the fake for a key with an expiry, one without, and a response with no key

## 2. Guard

- [x] 2.1 Add the `KeyInfo` interface, the per-key warning state, and the daily check in `Daily` before the heartbeat, recording each key's label and expiry (`never`, `unknown`) in the heartbeat (design D1, D2, D3); verify tests: the 30-day warning sent once, a run at 6 days with no earlier warning sends one 7-day alert, a rotated key resets, no warning for `never` or `unknown`, and a failed call doesn't stop the daily run
- [x] 2.2 Write the alert text (design D4); verify a test for its subject and body

## 3. Wiring

- [x] 3.1 In `guard/main.go`, give the guard both clients' `KeyInfo`; verify `make` and a unit test of the wiring if one exists

## 4. Docs

- [x] 4.1 In `docs/operating.md`'s rotation section, describe the warnings (thresholds, what `never` and `unknown` mean in the heartbeat); add a `CHANGELOG.md` entry with the new outbound `WhoAmI` call under Security-relevant; follow the docs' style (no em-dashes, Oxford comma)

## 5. Build and verify

- [x] 5.1 Run `goimports`, `make`, and `go test ./...`; verify all pass
- [x] 5.2 On the e2e stack, after the Jennah change is deployed, run the daily task and confirm the heartbeat shows both keys' labels and expiry; the e2e keys expire on 2026-12-10, so a run on or after 2026-11-10 sends the 30-day warning for both
