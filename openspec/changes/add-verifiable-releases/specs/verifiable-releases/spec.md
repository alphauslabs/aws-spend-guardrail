# Spec Delta

## Purpose

Lets a client confirm that the guard they deploy was built, unmodified, from the public source they reviewed: releases built by public CI, reproducible, signed, and checkable against the function actually running in their account.

## ADDED Requirements

### Requirement: Releases are built only by public CI from a version tag
A release SHALL be created only by the repository's public release workflow, triggered by a tag of the form `vMAJOR.MINOR.PATCH` on a commit in `main`. Every asset SHALL be built by that workflow run. The workflow's run log SHALL be publicly readable.

#### Scenario: A tag produces a release
- **WHEN** a maintainer pushes tag `v0.3.0` on a commit in `main`
- **THEN** the release workflow builds every asset and publishes release `v0.3.0` with them

#### Scenario: A tag off main does not release
- **WHEN** a tag `v0.3.1` is pushed on a commit that is not in `main`
- **THEN** the workflow fails and no release is published

### Requirement: A release has a fixed set of assets
Each release SHALL include the CloudFormation template, the guard function package, the `guardrail` CLI for linux-amd64, linux-arm64, darwin-amd64 and darwin-arm64, an SBOM, a `SHA256SUMS` file covering every other asset, and a signature bundle for `SHA256SUMS`.

#### Scenario: Every asset is covered
- **WHEN** a reviewer lists a release's assets
- **THEN** every asset except `SHA256SUMS` and its signature bundle has exactly one line in `SHA256SUMS`

### Requirement: Builds are reproducible
Building a release tag with the documented command, using the Go toolchain pinned in the repo, SHALL produce a function package and CLI binaries byte-identical to the release assets, on any machine.

#### Scenario: A client rebuilds a release
- **WHEN** a reviewer checks out tag `v0.3.0` and runs the documented build command
- **THEN** the SHA-256 of their function package and of each CLI binary matches the line in that release's `SHA256SUMS`

### Requirement: Checksums are signed and verifiable without a GitHub account
`SHA256SUMS` SHALL be signed by the release workflow with Sigstore keyless signing. The signature bundle SHALL be a release asset. Verification SHALL need only public tools and the documented signer identity (this repository's release workflow and the GitHub Actions OIDC issuer), not a GitHub account or a key from us.

#### Scenario: Verifying a signature
- **WHEN** a reviewer runs the documented verify command on `SHA256SUMS` and its bundle
- **THEN** it succeeds only if the file was signed by this repository's release workflow for that tag

#### Scenario: A changed checksum file fails
- **WHEN** one byte of `SHA256SUMS` is changed
- **THEN** the documented verify command fails

### Requirement: The package hash is published in Lambda's form
Each release's notes SHALL state the function package's SHA-256 both in hex and in the base64 form AWS Lambda reports as the function's code hash, so a reviewer can compare a deployed function directly.

#### Scenario: Comparing with a deployed function
- **WHEN** a reviewer reads a deployed guard function's code hash from AWS Lambda
- **THEN** it equals the base64 hash in the notes of the release it was deployed from

### Requirement: The template deploys a package the client chooses
The release template SHALL take the function package from an S3 bucket and key given as stack parameters, and SHALL NOT reference any bucket owned by us, so a client can deploy a package they verified or built themselves.

#### Scenario: Deploying a self-built package
- **WHEN** a client uploads their own build of `v0.3.0` to their bucket and deploys the `v0.3.0` template pointing at it
- **THEN** the stack deploys it, and the running function's code hash matches the release's published hash

### Requirement: The running guard reports its version and package hash
The guard SHALL read its own release version and code hash when it starts, and SHALL include both in every heartbeat and `config_applied` event it writes to Jennah.

#### Scenario: Audit from Jennah
- **WHEN** a reviewer reads the latest heartbeat event in an MSP's inbox
- **THEN** it names the guard's release version and the code hash of the function that wrote it

#### Scenario: A modified function
- **WHEN** the deployed function package is replaced by one not from a release
- **THEN** its heartbeat reports a code hash that matches no release

### Requirement: Every release has a changelog entry with a security-relevant section
`CHANGELOG.md` SHALL have an entry for every release. Each entry SHALL include a "Security-relevant" section listing changes to the freeze policies, IAM permissions, stack parameters that control approvals, and outbound network calls, or stating that there are none. The release workflow SHALL fail if the tag has no entry.

#### Scenario: A tag without an entry
- **WHEN** tag `v0.4.0` is pushed and `CHANGELOG.md` has no `v0.4.0` entry
- **THEN** the release workflow fails and nothing is published

### Requirement: Releases pass gates before publishing
The release workflow SHALL fail without publishing if a known vulnerability affects reachable code in the guard or CLI, if a second build of the function package or a CLI binary differs from the first, or if any third-party CI action is referenced by anything other than a full commit hash.

#### Scenario: A reachable vulnerability
- **WHEN** the vulnerability check reports a vulnerability in a function the guard calls
- **THEN** the workflow fails and no release is published

#### Scenario: A non-reproducible build
- **WHEN** the two builds of the function package in one run produce different hashes
- **THEN** the workflow fails and no release is published

### Requirement: A verification guide for reviewers
The repo SHALL include a verification guide that a reviewer without a GitHub account can follow from a release page: verify the signature on `SHA256SUMS`, check asset hashes, rebuild from source and compare, and compare the deployed function's code hash.

#### Scenario: Following the guide
- **WHEN** a reviewer follows the guide for a release
- **THEN** every command in it runs as written with only public tools and ends with a match or a clear mismatch
