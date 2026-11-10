# Spec Delta

## Purpose

Defines the guard's configuration: one versioned document per payer org, kept in Jennah, validated against a schema shipped in this repo, applied so that tightening is immediate and loosening needs a delay and a human approval.

## ADDED Requirements

### Requirement: Config is a versioned document
A payer org's config SHALL be a series of documents, each with a positive integer version. A version SHALL never change once written. The guard SHALL treat the highest version as the desired config.

#### Scenario: A newer version supersedes
- **WHEN** versions 3 and 4 exist for a payer org
- **THEN** the guard evaluates version 4 as the desired config

#### Scenario: Older versions stay readable
- **WHEN** version 4 is applied
- **THEN** versions 1 to 3 remain readable as history

### Requirement: The document declares scopes and their limits
A config document SHALL declare the payer account id it is for and a list of scopes. Each scope SHALL name one account or OU, a ceiling in USD, a budget period, a threshold percentage, a freeze style, a mode and a month-end setting. Omitted fields SHALL take documented defaults, and the document MAY set defaults for all scopes.

#### Scenario: Defaults fill a minimal scope
- **WHEN** a scope states only an account id and a ceiling of 20000 USD
- **THEN** it takes period `monthly`, threshold 100%, freeze style `B`, mode `dry-run` and month-end `stay_frozen`

### Requirement: Validation is shared and strict
The repo SHALL ship one JSON schema and one validator used by the CLI and by the guard. A document SHALL be invalid if it fails the schema, names another payer account, names a scope twice, names the payer account or the organization root, or sets a threshold outside 1 to 100.

#### Scenario: Same verdict everywhere
- **WHEN** a document is validated by the CLI and then pulled by the guard
- **THEN** both reach the same verdict with the same messages

#### Scenario: A duplicate scope is invalid
- **WHEN** a document names account `111122223333` in two scopes
- **THEN** validation fails naming the duplicated account

### Requirement: An invalid or unreachable config never weakens protection
If the newest version is invalid, or Jennah cannot be reached, the guard SHALL keep applying the last valid config it holds, SHALL record the problem when it can, and SHALL alert locally. The guard SHALL hold the last applied config in the payer account so it survives a restart.

#### Scenario: An invalid version is published
- **WHEN** version 5 fails validation while version 4 is applied
- **THEN** version 4 stays applied, a `config_rejected` event names version 5 and its errors, and a local alert is sent

#### Scenario: Jennah is unreachable after a restart
- **WHEN** the guard function starts and cannot reach Jennah
- **THEN** it reconciles from the last applied config held in the payer account and alerts locally

### Requirement: The guard pulls config on a schedule
The guard SHALL check for a newer config version at least every 5 minutes. Jennah SHALL never push to or call into the payer account.

#### Scenario: A new version is picked up
- **WHEN** a new valid, purely tightening version is published
- **THEN** it is applied within 5 minutes plus the reconcile time

### Requirement: Tightening applies at once
A change SHALL be tightening when, for every scope, it is at least as strict as before: a lower or equal ceiling or threshold, mode `enforce` instead of `dry-run`, freeze style `B` instead of `A`, `stay_frozen` instead of `auto_lift`, or a scope added. A purely tightening version SHALL be applied without delay or approval.

#### Scenario: A lower ceiling applies now
- **WHEN** a version lowers one scope's ceiling and changes nothing else
- **THEN** it is applied on the next pull, with no approval raised

### Requirement: Loosening needs a delay and an approval
Any other change SHALL be loosening, including a removed scope or a higher ceiling. When a version loosens, the guard SHALL at once apply its tightening parts, SHALL raise one Jennah approval for that version, and SHALL apply the full version only after the approval is approved and the configured delay has passed.

#### Scenario: A mixed version
- **WHEN** a version lowers scope X's ceiling and raises scope Y's ceiling
- **THEN** X's lower ceiling applies at once, Y keeps its old ceiling, and one approval is raised for the version

#### Scenario: Approved and delayed
- **WHEN** the approval for a loosening version is approved 10 minutes after the version was seen, and the delay is 30 minutes
- **THEN** the full version is applied 30 minutes after it was seen, not before

#### Scenario: Republished approval is not re-raised
- **WHEN** the guard pulls the same pending loosening version again
- **THEN** no second approval is raised for it

### Requirement: A rejected or expired approval keeps the stricter config
If the approval for a loosening version is rejected, expires or is cancelled, the guard SHALL NOT apply that version's loosening parts and SHALL record the outcome. A newer version SHALL supersede a pending one, and the pending approval SHALL then be cancelled.

#### Scenario: Nobody answers
- **WHEN** a loosening approval expires
- **THEN** the loosening is not applied and a `loosening_expired` event is recorded

#### Scenario: A newer version arrives
- **WHEN** version 7 is published while version 6's loosening approval is pending
- **THEN** the approval for version 6 is cancelled and version 7 is evaluated on its own

### Requirement: Loosening approvers are fixed in the payer account
The approvers for loosening, the number of approvals required, the approval window and the delay SHALL be stack parameters in the payer account, not part of the config document, so that someone able to write config in Jennah cannot choose who approves it.

#### Scenario: Config cannot name approvers
- **WHEN** a config document contains an approvers field
- **THEN** validation fails

### Requirement: The CLI validates and publishes config
The `guardrail` CLI SHALL validate a document file, show how it differs from the applied version, classify each difference as tightening or loosening, and publish it as the next version. Publishing SHALL refuse an invalid document and SHALL be safe to retry.

#### Scenario: Publish shows the classification
- **WHEN** an operator publishes a document that raises one ceiling
- **THEN** the CLI shows that change as loosening needing approval before it writes the new version

#### Scenario: A retried publish writes one version
- **WHEN** a publish is retried after a lost response
- **THEN** exactly one new version exists

### Requirement: The CLI fetches a config version for editing
The `guardrail` CLI SHALL fetch a config version as a document file that `validate`, `diff` and `publish` accept unchanged. By default it SHALL fetch the newest version. It SHALL also report the version the guard last recorded as applied, and say when the newest version is not yet applied.

#### Scenario: Pull, edit, publish
- **WHEN** an operator pulls the config, changes one ceiling and publishes the file
- **THEN** the published version differs from the pulled one only in that ceiling

#### Scenario: A pending loosening is visible
- **WHEN** version 6 is the newest and is waiting for its loosening approval, while version 5 is applied
- **THEN** pulling returns version 6 and reports that version 5 is applied and version 6 is pending

#### Scenario: A specific version
- **WHEN** an operator pulls version 3
- **THEN** the CLI returns version 3's document
