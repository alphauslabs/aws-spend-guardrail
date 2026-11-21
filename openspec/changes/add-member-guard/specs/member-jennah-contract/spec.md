# Spec Delta

## Purpose

Defines what the member guard reads from and writes to Jennah: its config and inbox datasets, its two narrow credentials, the events and heartbeat it records, and the approvals it raises, so a client's caps and every cutoff are kept as an auditable record.

## ADDED Requirements

### Requirement: Two datasets per member account
Each member guard SHALL use a config dataset and an inbox dataset in the MSP's Jennah enterprise, with ids `guardrail.<msp>.<payer>.<account>.config` and `guardrail.<msp>.<payer>.<account>.inbox`, where `<payer>` is the payer account of the organization the member account belongs to.

#### Scenario: A client's reach
- **WHEN** a client's admins hold dataset selector `guardrail.msp_a.123456789012.111122223333.*`
- **THEN** they reach that member account's two datasets, and no payer org's or other account's datasets

#### Scenario: Two member guards under one payer org
- **WHEN** accounts `111122223333` and `444455556666` in payer org `123456789012` each run a member guard
- **THEN** each reads and writes only its own pair of datasets

### Requirement: The member guard holds two credentials
The member guard SHALL use two Jennah credentials stored in its account. The reader SHALL read its config dataset and `guardrail.prices`, and create, read, and cancel approvals, and nothing else. The writer SHALL write its inbox dataset and nothing else. Neither SHALL write any config dataset or decide an approval.

#### Scenario: A stolen writer cannot loosen
- **WHEN** someone uses the member guard's writer credential to write a config version
- **THEN** Jennah refuses the write

#### Scenario: No reach into the payer org
- **WHEN** someone uses the member guard's reader credential to read `guardrail.msp_a.123456789012.config`
- **THEN** Jennah refuses the read

### Requirement: Every member guard action is an inbox event
The member guard SHALL write one inbox event for each cutoff, would-cutoff, lift, restored cutoff, applied or rejected config version, loosening approval raised, approved, rejected, or expired, and daily heartbeat. Each event SHALL carry a unique id, its time, its kind, the caller it concerns if any, the config version in force, and the member guard's release version and code hash.

#### Scenario: A cutoff event
- **WHEN** role `app-chatbot` is cut off for its daily cap
- **THEN** the inbox gains one `cutoff` event whose id is the cutoff id and whose detail names the caller, the window, the cap, and the total

### Requirement: Events survive Jennah being unreachable
The member guard SHALL queue events in its account when Jennah is unreachable and deliver each at most once when it is reachable again. Counting, cutoffs, and lifts SHALL NOT wait on Jennah.

#### Scenario: Jennah down during a cutoff
- **WHEN** Jennah is unreachable and a caller passes its cap
- **THEN** the caller is cut off within 2 minutes, and the cutoff event reaches the inbox once Jennah is reachable

### Requirement: The member guard keeps its last valid config
When Jennah is unreachable or the latest version is invalid, the member guard SHALL keep enforcing its last applied config, and SHALL alert once.

#### Scenario: An invalid version
- **WHEN** a version that fails validation is published
- **THEN** the member guard keeps the previous config, records a rejection event, and alerts once

### Requirement: Loosening approvals go to the account's approvers
A loosening approval SHALL go to the approvers listed in the member guard's stack, with the quorum rules the guard uses, and SHALL describe each loosening change in a sentence naming the caller and both values.

#### Scenario: A raise
- **WHEN** a version raises role `app-chatbot`'s daily cap from $20 to $40
- **THEN** the stack's approvers receive one approval that says so

### Requirement: The heartbeat states the member guard's coverage
The daily heartbeat SHALL list the caller entries in force, every cutoff in place, the regions covered, regions where the account made Bedrock calls but the member guard isn't installed, and SSO callers that would have been cut off.

#### Scenario: An uncovered region
- **WHEN** the account made Bedrock calls in eu-west-1 and the member guard is installed only for us-east-1
- **THEN** the heartbeat lists eu-west-1 as not covered
