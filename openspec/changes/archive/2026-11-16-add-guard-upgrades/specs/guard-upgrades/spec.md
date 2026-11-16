## Purpose

Lets the people who run guards in many payer orgs keep them current: each release says what upgrading to it takes, an upgrade or a rollback is safe by default, a guard says when its stack doesn't match its code, and operators can see which release each payer org runs.

## ADDED Requirements

### Requirement: Each release states its upgrade steps
Every release's CHANGELOG entry SHALL have a non-empty "Upgrade" section listing the actions upgrading to it needs, in order, or saying "None." The release workflow SHALL refuse to release a tag whose entry lacks it, and the release notes SHALL include it.

#### Scenario: A release with a new stack parameter
- **WHEN** a release adds a stack parameter that must be set to use a feature
- **THEN** its Upgrade section names the parameter, what to set, and whether `guardrail setup` must be rerun first

#### Scenario: No Upgrade section
- **WHEN** a tag is pushed whose CHANGELOG entry has no Upgrade section
- **THEN** the release workflow fails naming the missing section, and nothing is released

### Requirement: A release installs over any earlier release
A guard of a release SHALL run on the stored state, config versions, and inbox written by any earlier release without a manual step, unless the release's Upgrade section names one. It SHALL keep every freeze, cutoff, and pending approval it finds.

#### Scenario: An upgrade with a freeze in place
- **WHEN** a payer org with a frozen account and a pending loosening approval updates its stack to a newer release
- **THEN** the new guard keeps the freeze and the same pending approval, and its next heartbeat reports the new version

### Requirement: Rolling back to the previous release is safe by default
A guard of a release SHALL keep running correctly on state written by the next release, unless that next release's Upgrade section says a rollback to it isn't supported and why.

#### Scenario: A rollback after an upgrade
- **WHEN** a stack is updated to a release and then back to the one before it
- **THEN** the older guard keeps the freezes and cutoffs in place and runs its tasks without errors

### Requirement: The guard reports a stack template older than its code
The guard SHALL know the lowest template revision its code needs. On a stack whose template has a lower revision, it SHALL keep running, send one alert naming both revisions and that the stack must be updated with the release's template, and record both revisions in its heartbeat until the stack is updated.

#### Scenario: Only the package was updated
- **WHEN** a stack's function package is replaced with a release that needs template revision 3, and the stack still has revision 2
- **THEN** one alert says the template is older than the code needs, the heartbeat records revisions 2 and 3, and freezes and pulls continue

### Requirement: The guard reports a version label that doesn't match its code
When the stack's `GuardVersion` parameter differs from the release version built into the running code, the guard SHALL record both in its heartbeat and send one alert, and SHALL report the built-in version as its version.

#### Scenario: A forgotten parameter
- **WHEN** a stack runs the v0.2.0 package with `GuardVersion` still set to v0.1.0
- **THEN** the heartbeat reports guard version v0.2.0 and stack label v0.1.0, and one alert says the label is stale

### Requirement: Operators can list the version each payer org runs
The CLI SHALL list, for an MSP or one payer org, each payer org's running release, code hash, and newest heartbeat time, compared with the latest release. It SHALL mark each payer org as current, behind, silent (no heartbeat for over 2 days), or unknown, and SHALL work with only read access to the inboxes. When the latest release can't be read, it SHALL still list versions and say the comparison is unavailable.

#### Scenario: Two payer orgs, one behind
- **WHEN** an MSP has payer orgs running v0.2.0 and v0.1.0 and the latest release is v0.2.0
- **THEN** the listing shows the first as current and the second as behind, with their hashes and heartbeat times

#### Scenario: GitHub unreachable
- **WHEN** the latest release can't be read
- **THEN** every payer org's version is still listed, and the listing says it couldn't check for a newer release

### Requirement: The console shows each install's version
The console's guardrail page SHALL show each install's running release and mark installs behind the latest release, and a payer org's page SHALL say when a newer release is out and link to the upgrade guide.

#### Scenario: A newer release
- **WHEN** a payer org runs v0.1.0 and v0.2.0 is released
- **THEN** its row on the guardrail page is marked behind, and its page names v0.2.0 with a link to the upgrade guide

### Requirement: Guards never check for releases themselves
A guard SHALL NOT contact GitHub or any service other than AWS and Jennah to learn of releases.

#### Scenario: A guard behind the latest release
- **WHEN** a newer release is published
- **THEN** the guard's outbound calls are unchanged, and only the CLI and console report that it is behind
