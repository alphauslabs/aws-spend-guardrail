## MODIFIED Requirements

### Requirement: The guard holds two credentials
The guard SHALL use two Jennah credentials, stored in the payer account. The reader SHALL be able to read the config dataset and create, read and cancel approvals, and nothing else. The writer SHALL be able to write the inbox dataset, and nothing else. Neither SHALL be able to write the config dataset. A guard whose stack publishes prices SHALL also hold a price publisher credential, able to read and write the prices dataset and nothing else.

#### Scenario: A stolen writer cannot loosen
- **WHEN** someone uses the guard's writer credential to write a config version
- **THEN** Jennah refuses the write

#### Scenario: A stolen reader cannot loosen
- **WHEN** someone uses the guard's reader credential to write a config version or decide an approval
- **THEN** Jennah refuses both

#### Scenario: A stolen publisher cannot loosen
- **WHEN** someone uses the price publisher credential to write a config version, write the inbox, or decide an approval
- **THEN** Jennah refuses each

## ADDED Requirements

### Requirement: The publisher key's expiry is reported and warned
A guard that publishes prices SHALL report the price publisher key's expiry in the daily heartbeat and warn before it expires, exactly as for its reader and writer keys.

#### Scenario: The publisher key expires soon
- **WHEN** the daily run finds the publisher key expires in 13 days
- **THEN** one alert says the price publisher key expires on that date and must be rotated, and the heartbeat records its expiry

### Requirement: Setup prints the publisher key's reach
`guardrail setup` SHALL print the reach the price publisher credential needs (the prices dataset, read and write) alongside the guard credentials, saying it is needed only in the one payer org that publishes prices.

#### Scenario: A first setup
- **WHEN** an operator runs setup for a payer org
- **THEN** the output lists the reader, the writer, and the price publisher with their dataset selectors and permissions
