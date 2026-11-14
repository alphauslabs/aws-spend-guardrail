# Spec Delta

## Purpose

Defines what the guard reads from and writes to Jennah: the per-MSP datasets and tables, the two credentials and their reach, the event records, and how the guard behaves when Jennah is unreachable. It is the versioned interface the phase 2 learning agent will build on.

## ADDED Requirements

### Requirement: Two datasets per payer org
Each payer org SHALL use two datasets in the distributor's Jennah enterprise: a config dataset holding config versions, and an inbox dataset holding the guard's events. Their ids SHALL be `guardrail.<msp>.<payer>.config` and `guardrail.<msp>.<payer>.inbox`, so one MSP can have several payer orgs, and a dataset selector can reach one payer org or all of an MSP's.

#### Scenario: MSP reach by prefix
- **WHEN** a role holds dataset selector `guardrail.msp_a.*`
- **THEN** it reaches the config and inbox datasets of every payer org of MSP A, and no other MSP's

#### Scenario: One MSP with two payer orgs
- **WHEN** MSP A has payer accounts `123456789012` and `210987654321`, each with its own guard
- **THEN** each guard reads and writes only its own pair of datasets, and a role with selector `guardrail.msp_a.123456789012.*` reaches only the first pair

### Requirement: The table layouts are versioned in this repo
The tables of both datasets SHALL be defined in this repo with a layout version. A layout change SHALL only add tables or columns, so a guard written for an older layout keeps working against a newer one.

#### Scenario: An older guard on a newer layout
- **WHEN** the inbox gains a column a deployed guard does not know
- **THEN** the guard's writes still succeed

### Requirement: The guard holds two credentials
The guard SHALL use two Jennah credentials, stored in the payer account. The reader SHALL be able to read the config dataset and create, read and cancel approvals, and nothing else. The writer SHALL be able to write the inbox dataset, and nothing else. Neither SHALL be able to write the config dataset.

#### Scenario: A stolen writer cannot loosen
- **WHEN** someone uses the guard's writer credential to write a config version
- **THEN** Jennah refuses the write

#### Scenario: A stolen reader cannot loosen
- **WHEN** someone uses the guard's reader credential to write a config version or decide an approval
- **THEN** Jennah refuses both

### Requirement: Config versions are written once
Writing a config version SHALL fail if that version already exists, so two concurrent publishers cannot both write the same version.

#### Scenario: Concurrent publishers
- **WHEN** two operators publish version 8 at the same time
- **THEN** exactly one write succeeds and the other is told the version already exists

### Requirement: Every guard action is an inbox event
The guard SHALL write one inbox event for each freeze, would-freeze, lift, applied config version, rejected config version, loosening approval raised, approved, rejected or expired, drift, and daily heartbeat. Each event SHALL carry a unique id, its time, its kind, the scope it concerns if any, the config version in force, and the guard's version.

#### Scenario: A freeze event
- **WHEN** scope `111122223333` is frozen while config version 4 is applied
- **THEN** an event of kind `freeze` naming that scope, version 4, the ceiling, threshold and actual cost is written to the inbox

#### Scenario: The daily heartbeat
- **WHEN** a day passes with no other event
- **THEN** a `heartbeat` event is written, so silence in the inbox means the guard is not running

### Requirement: Events are written at most once
An event SHALL keep the same id across retries, and each write SHALL use that id as its idempotency key, so a retried write never produces a second event.

#### Scenario: A lost response
- **WHEN** an event write times out after Jennah committed it, and the guard retries
- **THEN** the inbox holds the event once

### Requirement: Events survive Jennah being unreachable
When an event cannot be written, the guard SHALL keep it in a queue in the payer account and retry until it succeeds. An event that still fails after the queue's retention SHALL be kept in a dead-letter queue and alerted locally, never dropped silently.

#### Scenario: Jennah is down for an hour
- **WHEN** Jennah is unreachable for an hour during which a freeze happens
- **THEN** the freeze event is written to the inbox once Jennah is reachable again, with its original time

### Requirement: Loosening approvals identify their version
An approval the guard raises for a loosening SHALL carry the payer account and config version in its payload, show the loosening differences in its summary, and use a request key derived from the payer account and version, so a retry never raises a second approval.

#### Scenario: Approval content
- **WHEN** version 6 raises scope Y's ceiling from 50000 to 60000 USD
- **THEN** the approval summary shows that change and its payload names the payer account and version 6

### Requirement: The setup command creates an MSP's datasets
The CLI SHALL create an MSP's two datasets and declare their tables, using an operator's own Jennah credentials. It SHALL be safe to run again, and SHALL print the dataset selectors and permissions each guard credential needs.

#### Scenario: Running setup twice
- **WHEN** setup is run twice for MSP `msp_a`
- **THEN** both datasets and their tables exist once, and the second run reports that nothing changed
