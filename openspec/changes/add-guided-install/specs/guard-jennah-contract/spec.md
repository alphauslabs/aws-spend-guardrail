# Spec Delta

## ADDED Requirements

### Requirement: The heartbeat reports the organization's SCP readiness
Each heartbeat SHALL carry an `org` section saying whether service control policies are enabled on the organization's root, and listing each scope target that has no free SCP slot for its freeze policy. When the guard could not check, the section SHALL say so instead of guessing.

#### Scenario: SCPs disabled
- **WHEN** a guard is installed in an organization whose root does not have service control policies enabled
- **THEN** its first heartbeat's `org` section says SCPs are not enabled

#### Scenario: A full target
- **WHEN** a scope names an OU that already has 10 SCPs attached directly, none of them a freeze policy
- **THEN** the heartbeat's `org` section lists that OU as having no free slot

#### Scenario: The check could not run
- **WHEN** the guard's call to list the organization's roots fails
- **THEN** the `org` section says the check was not done, and no other field of the section claims a state

### Requirement: The guard checks SCP readiness on its first run and when scopes change
The guard SHALL check SCP readiness on a new install's first run, at the daily run, and in any run that applies a version changing the set of scope targets. A change in the result SHALL count as a change of state for writing a heartbeat.

#### Scenario: SCPs turned on after install
- **WHEN** an organization's owner enables SCPs the day after the guard was installed
- **THEN** the next daily run's heartbeat says SCPs are enabled

#### Scenario: A new scope on a full target
- **WHEN** a version adds a scope whose target has no free SCP slot
- **THEN** the run that applies it writes a heartbeat listing that target

## MODIFIED Requirements

### Requirement: The guard holds two credentials
The guard SHALL use two Jennah credentials, stored in the payer account, either in secrets the operator created or in secrets the stack created from an install code. The reader SHALL be able to read the config dataset and create, read and cancel approvals, and nothing else. The writer SHALL be able to write the inbox dataset, and nothing else. Neither SHALL be able to write the config dataset. A guard whose stack publishes prices SHALL also hold a price publisher credential, able to read and write the prices dataset and nothing else.

#### Scenario: A stolen writer cannot loosen
- **WHEN** someone uses the guard's writer credential to write a config version
- **THEN** Jennah refuses the write

#### Scenario: A stolen reader cannot loosen
- **WHEN** someone uses the guard's reader credential to write a config version or decide an approval
- **THEN** Jennah refuses both

#### Scenario: A stolen publisher cannot loosen
- **WHEN** someone uses the price publisher credential to write a config version, write the inbox, or decide an approval
- **THEN** Jennah refuses each

#### Scenario: Keys from an install code
- **WHEN** a stack is created with an install code
- **THEN** the guard holds the same two credentials, with the same reach, read from the secrets the stack created
