# Proposal

## Why

The guard runs in a payer (management) account, the most sensitive account in an AWS organization. The repo is public so a client's security team can read the code without needing a GitHub account. Reading the source only helps if they can also confirm that what they deploy was built from that source. Today nothing links the two: there are no releases, no checksums and no way to check a deployed function against the code.

## What Changes

- **Releases come from tags, built by public CI.** Pushing a `vX.Y.Z` tag runs a public GitHub Actions workflow that builds and publishes the release. No release asset is built on a developer's machine.
- **Each release publishes:** the CloudFormation template, the guard function package, the `guardrail` CLI for Linux and macOS (amd64 and arm64), a `SHA256SUMS` file, an SBOM, and a signature bundle for `SHA256SUMS`.
- **Reproducible builds.** Building a tag with one documented command gives byte-identical function packages and CLI binaries, so a client can build from source and compare hashes instead of trusting ours.
- **Signatures verifiable without a GitHub account.** `SHA256SUMS` is signed with Sigstore keyless signing from the release workflow's identity, and the bundle is a release asset that standard tools can check offline against that identity.
- **The deployed guard reports what it is running.** The guard records its release version and its package hash in Jennah, in the same form AWS Lambda reports, so a client can compare the running function with a release without opening the AWS console.
- **A changelog that flags what reviewers care about.** Every release has a changelog entry with a "Security-relevant" section listing changes to freeze policies, IAM permissions and outbound calls, or stating there are none. A tag with no entry does not release.
- **Release gates:** a vulnerability check on reachable code, pinned CI actions and Go toolchain, and a reproducibility check that builds twice and compares.
- **A verification guide** (`docs/verify.md`) for client reviewers: check the signature, check the hashes, rebuild from source, compare with the running function.

Non-goals: AWS Lambda code signing (it needs an AWS Signer profile clients would have to trust; possible later), publishing to the AWS Serverless Application Repository or Marketplace, container images, and signing individual commits.

## Capabilities

### New Capabilities

- `verifiable-releases`: what a release contains, how it is built and signed, reproducibility, the changelog rule, release gates, the guard reporting its own version and package hash, and the verification guide.

### Modified Capabilities

None. `add-budget-freeze-guard` is not archived yet, so the guard's self-report is specified here as part of releases instead of as a change to `guard-jennah-contract`.

## Impact

- **Repo:** `.github/workflows/` (release and CI workflows), `CHANGELOG.md`, `docs/verify.md`, `Makefile` release targets, `go.mod` toolchain pin.
- **Guard:** reads its own package hash at start and adds version and hash to its heartbeat and `config_applied` events. Its role gains read access to its own function configuration.
- **Template:** takes the function package from a bucket and key the client controls, so they can deploy a package they verified or built themselves.
- **Depends on `add-budget-freeze-guard`** for the function, template, CLI and `Makefile`. The workflows can be written first, but the end-to-end check needs that change's group 1.
- **External services:** GitHub Actions and Releases, Sigstore (Fulcio and Rekor) for keyless signing. Nothing new in client accounts.
