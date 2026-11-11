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

### Requirement: Loosening approvals show the reason given
An approval the guard raises for a loosening SHALL show the config version's note in its summary as the reason given, and SHALL carry it in its payload. When the version has no note, the summary SHALL say that no reason was given. A note longer than 4,000 characters SHALL be shortened in the summary, saying it was shortened and naming the version that holds the full note. The `loosening_raised` event SHALL record the note.

#### Scenario: A raise with evidence
- **WHEN** version 7 raises a ceiling and its note reads "Month-end batch, on trend for 6 months; forecast reaches the ceiling tomorrow"
- **THEN** the approval's summary shows that text as the reason given, its payload carries it, and the `loosening_raised` event records it

#### Scenario: No reason given
- **WHEN** version 8 removes a scope and has no note
- **THEN** the approval's summary says no reason was given

#### Scenario: A very long note
- **WHEN** a version's note is 10,000 characters
- **THEN** the summary shows the first part of it, says it was shortened, and names the version holding the full note, and the approval is created
