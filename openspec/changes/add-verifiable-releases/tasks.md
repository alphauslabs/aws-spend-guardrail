# Tasks

## 1. Reproducible build

- [x] 1.1 Pin the Go toolchain in `go.mod` and `GOTOOLCHAIN` in the `Makefile`; verify `go version` inside `make` reports the pinned version
- [x] 1.2 Add `tools/zipdet`, a deterministic zip writer (fixed time from the tag's commit, fixed mode, single `bootstrap` entry, no extra fields); verify a unit test that zipping the same input twice, with different file mtimes, gives identical bytes
- [x] 1.3 Add `make release VERSION=vX.Y.Z` building the function package, the four CLI binaries and the template into `dist/` with the flags in design D2, and writing `SHA256SUMS`; verify two runs in fresh clones on different machines produce identical `SHA256SUMS`

## 2. Guard self-report

- [x] 2.1 Read the guard's own code hash at cold start with `lambda:GetFunction` on its own ARN, fall back to `unknown` with a local alert, and add version and hash to `heartbeat` and `config_applied` event details (design D5); verify unit tests for the normal and failure paths
- [x] 2.2 Add `lambda:GetFunction` on the guard's own function ARN to its role, and the `FunctionPackageBucket` and `FunctionPackageKey` parameters to the template (design D6); verify `cfn-lint` passes and the role grants nothing on other functions

## 3. CI and release workflows

- [x] 3.1 Add a CI workflow on push and pull request: `go test ./...`, `govulncheck ./...`, `cfn-lint`, and a check that every `uses:` is pinned to a full commit hash; verify it fails on a deliberately unpinned action in a test branch
- [x] 3.2 Add `tools/changelogcheck` and `CHANGELOG.md` with the required "Security-relevant" subsection (design D7); verify the check fails for a missing version and for a version without the subsection
- [x] 3.3 Add the release workflow on `v*` tags: confirm the tag's commit is in `main`, run the changelog check and `govulncheck`, build `dist/` twice in separate jobs and compare (design D3), generate the SBOM, sign `SHA256SUMS` with cosign keyless (design D1), and publish the release with notes giving the package hash in hex and base64 (design D4); verify on a test tag in a fork that the release appears with every asset
- [x] 3.4 Verify the gates with real tags: a tag not on `main` and a tag without a changelog entry each fail with nothing published (done on the repo itself with throwaway tags, since deleted). The non-reproducible-build gate is checked by review instead of a test: its `diff` step runs before signing and publishing in the same job, so a mismatch stops the job, and an empty second-build output differs from a full one, so it fails closed

## 4. Verification guide

- [ ] 4.1 Write `docs/verify.md`: install `cosign`, verify `SHA256SUMS`, check hashes, rebuild with `make release` and compare, read the deployed function's `CodeSha256`, and find the hash in the latest heartbeat in Jennah; link it from the README; verify every command as written against the test release from 3.3 on a machine with no GitHub login

## 5. Repository settings

- [x] 5.1 Add a tag ruleset: only maintainers may create, move or delete `v*` tags; `main` stays open to direct pushes by maintainers (design D9); verify a `v*` tag push, a tag move and a tag deletion by a non-maintainer are each refused, and a maintainer's direct push to `main` still works

## 6. Integration

- [ ] 6.1 Cut `v0.1.0` once `add-budget-freeze-guard` group 1 is applied, deploy it in the test organization from a self-built package, and confirm the signature verifies, the self-built hashes match the release, the Lambda `CodeSha256` matches the release notes, and the heartbeat in Jennah shows the same hash and version

## Workflow follow-up

- Archive after `v0.1.0` is verified end to end.
- Decide the SBOM format with the first client's reviewers (design open question).
