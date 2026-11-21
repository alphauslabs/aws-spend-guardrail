# Spec Delta

## Purpose

Releases the member guard on its own schedule from the same repo as the guard: its own tags, assets, changelog, and gates, lighter than the guard's, without changing or depending on guard releases.

## ADDED Requirements

### Requirement: Member releases are built only by public CI from a member tag
A member release SHALL be created only by the repository's public member release workflow, triggered by a tag of the form `member-vMAJOR.MINOR.PATCH` on a commit in `main`. Every asset SHALL be built by that workflow run. A `member-v*` tag SHALL NOT start the guard's release workflow, and a `v*` tag SHALL NOT start the member release workflow.

#### Scenario: A member tag
- **WHEN** a maintainer pushes `member-v0.1.0` on a commit in `main`
- **THEN** the member release workflow publishes release `member-v0.1.0`, and no guard release is created

#### Scenario: A member tag off main
- **WHEN** `member-v0.1.1` is pushed on a commit that is not in `main`
- **THEN** the workflow fails and nothing is published

### Requirement: A member release has a fixed set of assets
Each member release SHALL include the member CloudFormation template, the member function package, the `guardrail-member` CLI for linux-amd64, linux-arm64, darwin-amd64, and darwin-arm64, a `SHA256SUMS` file covering every other asset, and a Sigstore signature bundle for `SHA256SUMS`. It SHALL NOT include guard assets.

#### Scenario: Every asset is covered
- **WHEN** a reviewer lists a member release's assets
- **THEN** every asset except `SHA256SUMS` and its bundle has one line in `SHA256SUMS`, and none is the guard's template, package, or CLI

### Requirement: Member builds are reproducible and signed
Building a member tag with the documented command and the pinned Go toolchain SHALL produce a function package and CLI binaries byte-identical to the release assets, and the release workflow SHALL fail if its two builds differ. `SHA256SUMS` SHALL be signed with Sigstore keyless signing by the member release workflow.

#### Scenario: Verifying a member release
- **WHEN** a reviewer runs the documented verify command on a member release's `SHA256SUMS` and bundle
- **THEN** it succeeds only if the file was signed by the member release workflow for that tag

### Requirement: Member release notes publish the package hash
Each member release's notes SHALL state the member function package's SHA-256 in hex and in the base64 form AWS Lambda reports.

#### Scenario: Comparing with a deployed function
- **WHEN** a client reads the deployed member function's code hash from AWS Lambda
- **THEN** it equals the base64 hash in the notes of the release it was deployed from

### Requirement: Member releases have their own changelog
`member/CHANGELOG.md` SHALL have an entry for every member release with a "Security-relevant" section listing changes to the deny policies, IAM permissions, stack parameters that control approvals, and outbound network calls, or stating that there are none. The workflow SHALL fail if the tag has no entry or the entry lacks that section. An "Upgrade" section SHALL be optional.

#### Scenario: An entry without an Upgrade section
- **WHEN** `member-v0.2.0` is pushed and its entry has a Security-relevant section and no Upgrade section
- **THEN** the release is published

#### Scenario: No entry
- **WHEN** `member-v0.2.0` is pushed and `member/CHANGELOG.md` has no entry for it
- **THEN** the workflow fails and nothing is published

### Requirement: Member releases pass the light gates
The member release workflow SHALL fail without publishing if a known vulnerability affects reachable code in the member function or CLI, or if a third-party CI action is referenced by anything other than a full commit hash. It SHALL NOT require state fixtures, a rollback check against the previous release, or an SBOM.

#### Scenario: A reachable vulnerability
- **WHEN** the vulnerability check reports a vulnerability in a function the member guard calls
- **THEN** the workflow fails and no member release is published

### Requirement: Member releases are never the latest release
A member release SHALL be published without being marked as the repository's latest release.

#### Scenario: A member release after a guard release
- **WHEN** guard `v0.3.0` is released and then `member-v0.1.0`
- **THEN** GitHub's latest release is still `v0.3.0`

### Requirement: The member CLI finds the latest member release
The `guardrail-member` CLI SHALL compare a member guard's running version with the newest `member-vX.Y.Z` release, ignoring guard releases. Member guards SHALL NOT contact GitHub themselves.

#### Scenario: A guard release is newer
- **WHEN** the newest release overall is guard `v0.4.0` and the newest member release is `member-v0.1.0`
- **THEN** a member guard running `member-v0.1.0` is shown as current
