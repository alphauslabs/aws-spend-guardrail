# Spec Delta

## ADDED Requirements

### Requirement: Each release's template and package are hosted
The release workflow SHALL upload each release's template and function package to the hosted release bucket, a public, read-only S3 bucket we own in us-east-1, under `guard/<version>/`, after the release is signed. Each hosted file SHALL be byte-identical to the release asset of the same name.

#### Scenario: A release is hosted
- **WHEN** release `v0.3.0` is published
- **THEN** `guard/v0.3.0/template.yaml` and `guard/v0.3.0/guard.zip` are readable from the hosted bucket without credentials, and their SHA-256 match the lines in that release's `SHA256SUMS`

#### Scenario: A failed upload does not publish a half release
- **WHEN** the upload of `guard.zip` fails
- **THEN** the workflow fails, and the template of that version is not left in the hosted bucket without its package

### Requirement: Hosted files are never replaced
A version's files in the hosted bucket SHALL NOT be overwritten or deleted by the release workflow. A workflow run for a version already hosted SHALL fail rather than replace it.

#### Scenario: A re-run of a release
- **WHEN** the release workflow runs again for `v0.3.0`, which is already hosted
- **THEN** it fails, and the hosted files of `v0.3.0` are unchanged

### Requirement: Hosted files can be checked against the release
The verification guide SHALL show how to check a hosted template and package against a release's signed `SHA256SUMS`, and how to compare a guard deployed from the hosted package with the release's published code hash.

#### Scenario: A reviewer checks the hosted package
- **WHEN** a reviewer downloads `guard/v0.3.0/guard.zip` from the hosted bucket and follows the guide
- **THEN** its SHA-256 matches the `guard.zip` line of the signed `SHA256SUMS` of `v0.3.0`

## MODIFIED Requirements

### Requirement: The template deploys a package the client chooses
The release template SHALL take the function package from an S3 bucket and key given as stack parameters, and SHALL NOT reference any bucket owned by us, so a client can deploy a package they verified or built themselves. The hosted release bucket SHALL be named only by links and guides, never as a template default.

#### Scenario: Deploying a self-built package
- **WHEN** a client uploads their own build of `v0.3.0` to their bucket and deploys the `v0.3.0` template pointing at it
- **THEN** the stack deploys it, and the running function's code hash matches the release's published hash

#### Scenario: Deploying the hosted package
- **WHEN** a client deploys the `v0.3.0` template with `FunctionPackageBucket` set to the hosted bucket and `FunctionPackageKey` `guard/v0.3.0/guard.zip`
- **THEN** the stack deploys it, and the running function's code hash matches the release's published hash
