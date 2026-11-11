# guard-jennah-contract Specification

## Purpose
Defines what the guard reads from and writes to Jennah: the per-MSP datasets and tables, the two credentials and their reach, the event records, and how the guard behaves when Jennah is unreachable. It is the versioned interface the phase 2 learning agent will build on.

## Requirements

### Requirement: Inbox events are kept for 400 days
The inbox's events table SHALL declare a retention of 400 days on the event time, so Jennah removes an event some time after it is 400 days old, without the guard or an operator deleting it. Retention SHALL be a lower bound: an event SHALL NOT be removed before it is 400 days old. The config dataset SHALL declare no retention.

#### Scenario: An old event expires
- **WHEN** an event in a payer org's inbox is 401 days old
- **THEN** it is removed within a few days, with no request from the guard or an operator

#### Scenario: A recent event is kept
- **WHEN** an event is 399 days old
- **THEN** it is still in the inbox

#### Scenario: Config versions are kept
- **WHEN** a config version was published more than 400 days ago
- **THEN** it is still in the config dataset

### Requirement: Setup applies the retention to an existing inbox
The setup command SHALL apply the inbox retention to an inbox whose events table has a different retention or none, and SHALL say before applying it that events older than 400 days will be removed. An inbox that already has the retention SHALL be left unchanged, and a second run SHALL still report that nothing changed.

#### Scenario: An inbox created before retention
- **WHEN** setup runs for a payer org whose inbox was created without a retention
- **THEN** the retention is applied, and setup's output says that events older than 400 days will be removed

#### Scenario: Running setup again
- **WHEN** setup runs again for the same payer org
- **THEN** no schema change is made, and setup reports that nothing changed
