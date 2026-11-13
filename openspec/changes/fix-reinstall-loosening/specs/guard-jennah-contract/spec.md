## ADDED Requirements

### Requirement: A new install resumes from the version last in force
On its first run with no stored state, the guard SHALL find, from the inbox, the last config version a guard for this payer org applied in full, SHALL put that version in force at once, and SHALL then handle the latest version as a change from it: its tightening parts apply at once and its loosening parts wait for approval and the loosening delay, as for any new version.

#### Scenario: Reinstall while a loosening is pending
- **WHEN** version 9 (ceiling 6.50 USD) was applied in full, version 10 raises the ceiling to 7.00 USD and is awaiting approval, and the stack is deleted and recreated
- **THEN** the new guard puts version 9 in force with the 6.50 USD ceiling, and applies version 10 only after its approval is approved and the loosening delay has passed

#### Scenario: Reinstall after a rejected loosening
- **WHEN** version 10's loosening was rejected, so version 9 stayed in force, and the stack is deleted and recreated
- **THEN** the new guard puts version 9 in force and does not apply version 10's loosening

#### Scenario: Reinstall with nothing pending
- **WHEN** version 10 was applied in full and is the latest version, and the stack is deleted and recreated
- **THEN** the new guard puts version 10 in force at once, with no approval

### Requirement: A first install applies the latest version at once
When the inbox records no version applied in full for the payer org, the guard's first run SHALL apply the latest valid version at once, with no approval.

#### Scenario: First install
- **WHEN** a guard is installed for a payer org whose inbox records no version applied in full
- **THEN** the latest valid version applies at once, with no approval

### Requirement: A pending approval is reused across a reinstall
When a reinstalled guard needs approval for a version whose approval was already raised before the reinstall, it SHALL use that existing approval and SHALL NOT ask approvers a second time. The loosening delay SHALL be counted from when the new guard first saw the version.

#### Scenario: Approval raised before the reinstall
- **WHEN** version 10's approval was raised at 01:45 UTC, the stack is recreated at 02:20 UTC, and the approval is approved at 02:30 UTC
- **THEN** no second approval request reaches the approvers, and version 10 applies once the loosening delay has passed since 02:20 UTC

### Requirement: A new install is recorded in the inbox
On its first run with no stored state, the guard SHALL write a `guard_installed` event once, giving the version last applied in full before the install (or none), the latest version, and what the guard did: resumed from the earlier version, installed fresh, or could not check. Events the guard writes about config versions SHALL be recorded even when an earlier install wrote an event of the same kind for the same version.

#### Scenario: Reinstall is visible
- **WHEN** the stack is deleted and recreated, and the new guard applies version 10, which the old guard had also applied
- **THEN** the inbox has a `guard_installed` event saying it resumed from version 10, and a second `config_applied` event for version 10 from the new install, beside the old one

#### Scenario: A restart is not an install
- **WHEN** an installed guard's function restarts with its state intact
- **THEN** no `guard_installed` event is written

### Requirement: The reader key reads the inbox
The guard's reader key SHALL be able to read the payer org's inbox, and SHALL NOT be able to write it. When the reader key can't read the inbox, a new install SHALL alert once that it could not check which version was in force, SHALL apply the latest valid version at once, and SHALL record the check as not done in its `guard_installed` event.

#### Scenario: Setup mints the reader
- **WHEN** setup creates the reader key for a payer org
- **THEN** the key can read the config dataset, the shared prices dataset, and the payer org's inbox, and can write none of them

#### Scenario: Reader key minted before this change
- **WHEN** a guard is reinstalled with a reader key that can't read the inbox
- **THEN** one alert says the reinstall check could not run and how to fix the key, the latest valid version applies, and the `guard_installed` event says the check was not done
