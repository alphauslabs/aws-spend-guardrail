# Spec Delta

## ADDED Requirements

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
