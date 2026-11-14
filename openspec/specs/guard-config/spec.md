# guard-config Specification

## Purpose
Defines the guard's configuration: a versioned document per payer org, kept in Jennah, that declares the scopes the guard protects and their limits, the accounts left out on purpose, the alert recipients, and the AI guard's settings. It covers how a document is validated, how the guard pulls and applies it (tightening at once, loosening only after a delay and a person's approval), why an invalid or unreachable config never weakens protection, and how operators read, check, and publish versions with the CLI.

## Requirements
### Requirement: A document can't enforce more scopes than AWS allows budget actions
A document SHALL be invalid when more than 100 of its scopes are in `enforce` once defaults are applied, because AWS allows at most 100 budget actions per account and each enforcing scope needs one in the payer account. The problem SHALL be reported on `scopes`, give the number of enforcing scopes and the limit, and say to group accounts into OU scopes or set some scopes to `dry-run`. Scopes in `dry-run` SHALL NOT count.

#### Scenario: 101 enforcing scopes are refused
- **WHEN** a document has 101 account scopes and `defaults.mode` is `enforce`
- **THEN** validation fails on `scopes`, saying 101 scopes enforce and at most 100 can

#### Scenario: Dry-run scopes don't count
- **WHEN** a document has 150 scopes, 100 of them in `enforce` and 50 in `dry-run`
- **THEN** the document is valid

#### Scenario: A guard doesn't apply a refused version
- **WHEN** a version with 101 enforcing scopes is published to Jennah by a tool that skips validation
- **THEN** the guard refuses it as invalid, as for any invalid version, and keeps the config it has

### Requirement: The document lists alert recipients
The config document SHALL accept an optional `alert_emails`: a list of up to 20 distinct email addresses. Validation SHALL refuse an entry that isn't an email address, a duplicate (compared case-insensitively), and more than 20 entries. A document without `alert_emails` SHALL mean no recipients beyond the stack's `AlertEmail`.

#### Scenario: Two recipients
- **WHEN** a document sets `alert_emails` to `["oncall@example.com", "finops@example.com"]`
- **THEN** it validates, and both are the version's recipients

#### Scenario: A malformed list
- **WHEN** a document lists `Ops <ops@example.com>`, lists the same address twice, or lists 21 addresses
- **THEN** validation fails naming the entry or the count

### Requirement: Any recipient change needs approval
Adding or removing an alert recipient SHALL be handled like loosening: the guard SHALL raise the version's approval, SHALL keep the recipients in force unchanged until the approval is approved and the delay has passed, and SHALL apply the version's tightening parts at once as for any loosening version. A version whose recipients are its only change SHALL still need the approval. The approval SHALL list each added and removed recipient.

#### Scenario: Adding a recipient
- **WHEN** a version adds `finops@example.com` and changes nothing else
- **THEN** one approval is raised listing `alert recipient added: finops@example.com`, and the address is not subscribed until the approval is approved and the delay has passed

#### Scenario: A first config with recipients
- **WHEN** the first version a guard sees lists `finops@example.com`
- **THEN** its scopes apply at once, and the recipient waits for approval like any addition

#### Scenario: Removing a recipient alongside a tightening
- **WHEN** a version removes `oncall@example.com` and lowers scope X's ceiling
- **THEN** X's lower ceiling applies at once, `oncall@example.com` keeps receiving alerts, and one approval is raised listing the removal

#### Scenario: Rejected
- **WHEN** the approval for a recipient change is rejected or expires
- **THEN** the recipients in force stay as they were, and the outcome is recorded as for any loosening

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

### Requirement: An outage is alerted once, not on every pull
When refreshing config from Jennah starts failing, the guard SHALL publish one local alert. While it keeps failing, it SHALL publish a reminder at most every 6 hours, saying since when. When a refresh succeeds again, it SHALL publish one alert saying it recovered and for how long it was failing. The outage state SHALL survive a restart.

#### Scenario: A two-hour outage
- **WHEN** every pull fails for two hours, then one succeeds
- **THEN** exactly two alerts are sent: one when the failures start, and one on recovery naming the duration

#### Scenario: A long outage
- **WHEN** pulls keep failing for 13 hours
- **THEN** one alert is sent at the start and reminders after 6 and 12 hours, and no others

#### Scenario: A restart during an outage
- **WHEN** the function restarts while pulls are failing
- **THEN** it does not send a new "started failing" alert

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

### Requirement: The document configures the AI pack
The config document SHALL accept an optional `ai` section with: AI scopes per account or OU, each with hourly, daily, and monthly caps in USD and a mode; defaults for those; model and region allowlists; the callers allowed to use Bedrock API keys; per-model price overrides; and `lifts`, a list of cutoff ids to lift.

#### Scenario: A minimal AI section
- **WHEN** a document sets `ai.defaults.monthly_usd` to 500 and lists one account under `ai.scopes`
- **THEN** it validates, and that account has a $500 monthly cap in dry-run mode

### Requirement: The AI section is validated
Validation SHALL refuse an unknown field in the `ai` section, a negative cap, a scope that names an account or OU twice, a price override with a missing or negative price, and a lift naming no cutoff id.

#### Scenario: An invalid AI section
- **WHEN** a document gives a scope a negative hourly cap
- **THEN** validation fails naming the field

### Requirement: AI changes are classified like other changes
A lower cap, a model or region removed from an allowlist, a caller removed from the API-key list, a higher price override, and a move from dry-run to enforce SHALL be tightening and apply at once. A higher or removed cap, an added model, region, or API-key caller, a lower or removed price override, a move from enforce to dry-run, and a new lift SHALL be loosening and need the approval and delay.

#### Scenario: Adding a model
- **WHEN** a version adds a model to the allowlist
- **THEN** it waits for approval, and the approval lists the added model

#### Scenario: Lifting a cutoff
- **WHEN** a version lists cutoff `ai:111122223333:20261111T0930` under `ai.lifts`
- **THEN** one approval is raised for it, showing the cutoff and the version's reason
