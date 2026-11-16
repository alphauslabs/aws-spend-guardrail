# Tasks

## 1. Releases

- [ ] 1.1 Require a non-empty `### Upgrade` section in `tools/changelogcheck` and print it first with `-notes` (design D1); tests for a missing, empty, and present section
- [ ] 1.2 Add an Upgrade section to the `[Unreleased]` CHANGELOG entry, and document the section in the CHANGELOG's header notes
- [ ] 1.3 In `release.yml`, fail when `internal/guard/testdata/state/<tag>.json` is missing, and run the previous tag's `TestFixtures` against the new fixture (design D3)

## 2. Guard

- [ ] 2.1 Add `TEMPLATE_REVISION` to the template and `MinTemplateRevision` to the code, a test that they match, and the cold-start check with its once-per-pair alert and heartbeat fields (design D2); tests for a missing revision, a lower one, an equal one, and a higher one
- [ ] 2.2 Compare `GUARD_VERSION` with the built-in version at cold start, record `stack_guard_version`, and alert once per pair (design D4); tests for a match, a mismatch, and `dev`
- [ ] 2.3 Write a heartbeat on the first run after the code hash changes, so an upgrade shows up at once (design Risks); test
- [ ] 2.4 Add the state fixture helper, write the fixture for the next release, and `TestFixtures` loading every fixture with a freeze, a cutoff, and a pending approval kept (design D3)

## 3. Versions

- [ ] 3.1 In `internal/jennah`, list an MSP's inbox datasets (paged) and read each newest heartbeat's version, hash, and time (design D6); tests against the fake
- [ ] 3.2 Add the latest-release lookup with semantic comparison and `--latest` (design D5); tests with a fake HTTP server for a release, a failure, and non-semantic versions
- [ ] 3.3 Add `guardrail versions` with statuses, behind-first order, and `--json` (design D6); CLI tests for current, behind, silent, unknown, and a failed lookup

## 4. Console (jennah repo)

- [ ] 4.1 Add the Version column and "behind" mark to the guardrail page's installs table, and the newer-release notice with the guide link on a payer page, with the latest release cached for an hour (design D5, D7); tests for the comparison helper

## 5. Docs and build

- [ ] 5.1 Write `docs/upgrade.md` (design D8); replace the "New release" bullet in `docs/install-console.md` with a link to it; link it from `docs/install.md` and `docs/verify.md`; add CHANGELOG entries; follow the docs' style (no em-dashes, Oxford comma)
- [ ] 5.2 Run `goimports`, `make`, `go vet ./...`, `go test ./...`, and the console's `npm test`; verify all pass

## 6. Live check

- [ ] 6.1 On e2e: update only the function package to this build with the old template and confirm one template alert and the heartbeat fields; update the stack with the new template and confirm they clear; set `GuardVersion` wrong and confirm one alert; run `guardrail versions --msp e2e` and check the console shows the version
