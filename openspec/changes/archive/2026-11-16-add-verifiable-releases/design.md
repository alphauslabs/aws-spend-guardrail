# Design

## Context

See proposal.md for why. The repo is public on GitHub, Go-based, and has no code, CI or releases yet. `add-budget-freeze-guard` defines the artifacts this change releases: a Go Lambda (`provided.al2023`, arm64), a CloudFormation template, and the `guardrail` CLI, built by a root `Makefile`.

The audience is a client's security reviewer. They may not have a GitHub account, may not trust our infrastructure, and will usually check a release once, on upgrade.

## Goals / Non-Goals

**Goals:**
- One clear chain: source at a tag, public build, signed checksums, deployed function, the hash recorded in Jennah.
- Every check runs with public tools and no account.

**Non-Goals:**
- A build service other than GitHub Actions.
- SLSA level certification. We follow its ideas (provenance, isolated builds) without claiming a level.

## Decisions

### D1. Sigstore keyless signing of `SHA256SUMS`, verified with `cosign`
The workflow signs `SHA256SUMS` with `cosign sign-blob --bundle SHA256SUMS.sigstore.json`, using the workflow's GitHub OIDC identity. Reviewers run:

```
cosign verify-blob SHA256SUMS --bundle SHA256SUMS.sigstore.json \
  --certificate-identity "https://github.com/alphauslabs/aws-spend-guardrail/.github/workflows/release.yml@refs/tags/vX.Y.Z" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Signing one checksum file covers every asset with one signature and one command.

*Alternatives:* GitHub artifact attestations alone (`gh attestation verify`). Rejected as the main path: the usual verification flow goes through the `gh` CLI and GitHub's API, which works against the "no account needed" goal. We may add attestations as well, since they're cheap, but the guide doesn't depend on them. A long-lived signing key: rejected, because then key custody becomes something clients have to trust.

### D2. Reproducible Go builds
- `go.mod` pins the toolchain (`toolchain goX.Y.Z`), and `GOTOOLCHAIN` is set to that version in the `Makefile`.
- Builds use `CGO_ENABLED=0 -trimpath -buildvcs=false -ldflags "-buildid= -s -w -X main.version=$(VERSION)"`.
- The function package is a zip containing only `bootstrap`, written by a small Go program in `tools/zipdet` with a fixed modification time (the tag's commit time), fixed permissions and no extra fields. This avoids differences between `zip` versions.
- `make release VERSION=vX.Y.Z` builds everything into `dist/` and writes `SHA256SUMS`. CI and reviewers run the same target.

*Alternative:* building in a pinned container image. Rejected for now: the Go toolchain pin plus a deterministic zip writer is enough, and it doesn't ask reviewers to trust an image. Revisit if a dependency ever needs cgo.

### D3. Two builds per run as the reproducibility gate
The release workflow builds `dist/` twice in separate jobs on fresh runners and compares `SHA256SUMS`. A mismatch fails the run. This catches nondeterminism before a reviewer does.

### D4. Lambda's hash form in the release notes
AWS Lambda reports `CodeSha256` as the base64 encoding of the package's raw SHA-256. The workflow computes both forms and writes them in the release notes. The guide shows `aws lambda get-function --function-name <name> --query Configuration.CodeSha256`.

### D5. The guard reports itself
At cold start the guard calls `lambda:GetFunction` on its own ARN (from the Lambda context) to read `CodeSha256`. Its version is the `-X main.version` value. Both go into the `detail` of every `heartbeat` and `config_applied` event. If `GetFunction` fails, the guard reports `code_sha256: "unknown"` and alerts locally rather than failing. Its role gains `lambda:GetFunction` scoped to its own function ARN.

*Alternative:* hashing its own binary at runtime. Rejected: that's the binary, not the deployed package, so it can't be compared with Lambda's hash or the release.

### D6. Package location is a client parameter
Template parameters `FunctionPackageBucket` and `FunctionPackageKey`. The guide shows uploading either the release zip or their own build. Clients usually deploy from their own artifact bucket anyway.

### D7. Changelog gate
`CHANGELOG.md` follows "Keep a Changelog" headings, plus a required `### Security-relevant` subsection per version. A small check in `tools/changelogcheck` fails the run if the tag's section or its security subsection is missing.

### D8. CI hygiene
Third-party actions are pinned by full commit hash, and a CI check rejects any `uses:` line without one. `govulncheck ./...` runs on every push and as a release gate. Workflow permissions are least-privilege: `contents: write` and `id-token: write` only in the release job.

### D9. Release tags are protected; `main` is not
A repository ruleset restricts who can create, move or delete `v*` tags. `main` has no review requirement: maintainers push to it directly. What a release contains is still gated by the release workflow (tag on `main`, changelog, vulnerability check, reproducibility), and a reviewer audits a release by its diff from the previous tag. These are repository settings, not code. The task list records them so they're set and checked.

*Alternative:* also require pull request review on `main`. Rejected for now: it would stop the direct-push workflow the maintainers use. It can be added later if a client asks for "every change reviewed".

## Risks / Trade-offs

- [GitHub Actions or a pinned action is compromised] → Pinning by hash limits drift. Reproducible builds let a reviewer rebuild without trusting CI at all, which is the real backstop.
- [Sigstore is unavailable at release time] → The release fails and is retried. Nothing is published unsigned.
- [A Go toolchain update changes the output] → Expected. Reproducibility is per tag with the toolchain pinned at that tag.
- [The guard can't read its own hash, for example because of a restrictive permissions boundary] → It reports `unknown` and alerts. The guide also shows how to read the hash directly from Lambda.
- [Changes reach `main` without a second reviewer] → Each release's "Security-relevant" changelog section and the diff between tags give reviewers a bounded set of changes to audit. Review on `main` can be required later (design D9).
- [Reviewers skip verification] → Nothing forces them to. The guide keeps it to a few commands, and the hash in Jennah lets the check happen later from the audit record.

## Migration Plan

No existing releases. The first tag after this change and `add-budget-freeze-guard` group 1 are applied becomes `v0.1.0`. The tag ruleset is set before that tag.

## Open Questions

- Which SBOM format to publish (SPDX or CycloneDX). Either fits the requirement. Pick whichever the first client's tooling reads.
