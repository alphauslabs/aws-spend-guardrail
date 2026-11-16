## ADDED Requirements

### Requirement: Local alerts name the accounts they mention
Every local alert SHALL show each standalone account id that is an account in the organization as `<id> (<account name>)`, in the subject and the body. Ids inside ARNs or other ids, and the `Error:` line, SHALL be left exactly as they are. Inbox events SHALL NOT change.

#### Scenario: An alert about a member account
- **WHEN** account 730335604511, named `e2enormal`, is cut off by the AI guard
- **THEN** the subject reads `guardrail: 730335604511 (e2enormal) cut off` and the body names it as `account 730335604511 (e2enormal)`, while the cutoff id `ai:730335604511:20261114T1457` in the body is unchanged

#### Scenario: Error text and ARNs
- **WHEN** an alert's error line contains `arn:aws:sts::730335604511:assumed-role/x`
- **THEN** that line is sent exactly as written, with no name added

### Requirement: Naming accounts never holds up an alert
Reading account names SHALL NOT delay or prevent an alert: when names can't be read, the alert SHALL be sent with bare ids. When names would make the subject longer than SNS allows, the subject SHALL keep bare ids and the body SHALL still name them.

#### Scenario: Names can't be read
- **WHEN** the guard can't list the organization's accounts
- **THEN** the alert is sent at once with bare ids, and a later alert tries to read names again

#### Scenario: A long name
- **WHEN** an account's name would make the subject longer than 100 characters
- **THEN** the subject is sent with the bare id and the body names the account
