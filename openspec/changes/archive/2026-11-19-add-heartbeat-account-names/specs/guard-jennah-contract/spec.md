## ADDED Requirements

### Requirement: The heartbeat names the accounts it mentions
Each heartbeat SHALL carry the organization's name for each account its state mentions, when the guard can list the organization's accounts. A change in names alone SHALL NOT write a heartbeat, and a failure to list names SHALL NOT stop a heartbeat from being written.

#### Scenario: A cut-off account
- **WHEN** account 730335604511, named e2enormal, is cut off from AI
- **THEN** the heartbeat lists 730335604511 under the cutoffs and names it e2enormal

#### Scenario: Names can't be listed
- **WHEN** the guard can't list the organization's accounts on a run that writes a heartbeat
- **THEN** the heartbeat is written with ids only

#### Scenario: A rename
- **WHEN** an account mentioned in the state is renamed and nothing else changes
- **THEN** no heartbeat is written for it, and the next heartbeat carries the new name
