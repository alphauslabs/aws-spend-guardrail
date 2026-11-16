## ADDED Requirements

### Requirement: Loosening approvals name the accounts they change
A loosening approval request SHALL show each standalone account id in its list of changes and in its freeze lines as `<id> (<account name>)`, by the same rules and from the same name source as local alerts. The payer id, the reason given, and the payload SHALL keep bare ids. When names can't be read, the request SHALL be raised at once with bare ids.

#### Scenario: Removing the AI guard from a member account
- **WHEN** a version removes the AI scope of account 730335604511, named `e2enormal`
- **THEN** the approval reads `Stop the AI guard on account 730335604511 (e2enormal).`, while its payload lists `account:730335604511` and its title names the payer by id only

#### Scenario: Names can't be read
- **WHEN** the guard can't list the organization's accounts while raising an approval
- **THEN** the approval is raised with bare ids
