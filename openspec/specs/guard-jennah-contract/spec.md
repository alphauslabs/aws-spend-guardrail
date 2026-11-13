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

### Requirement: Loosening approvals carry a quorum rule Jennah accepts
An approval the guard raises for a loosening SHALL state a quorum rule. When the stack requires one approval, the rule SHALL be "any one approver". When the stack requires as many approvals as it lists approvers, the rule SHALL be "all approvers", and the required count SHALL equal the number of approvers. Approvers SHALL be counted once each, ignoring blanks.

#### Scenario: One approval required
- **WHEN** the stack lists one or more approvers and requires 1 approval
- **THEN** the approval is raised with the rule "any one approver", and Jennah accepts it

#### Scenario: Every approver required
- **WHEN** the stack lists two approvers and requires 2 approvals
- **THEN** the approval is raised with the rule "all approvers" and a required count of 2, and Jennah accepts it

### Requirement: An unsupported approval count is a setup error
When the stack's required approvals is neither 1 nor the number of approvers, the guard SHALL NOT send an approval request. It SHALL report a setup error naming the required count, the number of approvers, and the two values that work, through the same alert and reminders as any other failure to apply a config version. The stricter config SHALL stay in force, and protection that needs no approval SHALL keep working.

#### Scenario: Two of three approvers
- **WHEN** the stack lists three approvers, requires 2 approvals, and a published version loosens a ceiling
- **THEN** no approval is raised, the alert says 2 of 3 is not supported and that the value must be 1 or 3, the previous ceiling stays in force, and any tightening in that version applies

#### Scenario: More approvals than approvers
- **WHEN** the stack lists one approver and requires 2 approvals, and a version loosens anything
- **THEN** no approval is raised, and the alert says the value must be 1

### Requirement: Each freeze is its own inbox event
Every time a scope is frozen, the guard SHALL write a `freeze` event whose id names the scope and when that freeze began, so a scope frozen again after a lift in the same budget period gets a second event. A repeat of a freeze already in force SHALL reuse that freeze's id, so the inbox holds it once, and SHALL NOT send a second local alert. A `would_freeze` event SHALL stay at one per scope per budget period.

#### Scenario: Frozen, lifted, and frozen again in one month
- **WHEN** a scope is frozen on November 11 at 20:17, lifted at 21:15, and frozen again at 21:40
- **THEN** the inbox holds two `freeze` events and one `lift` event for it, and the latest is a `freeze`

#### Scenario: Budgets fires on a scope the guard already froze
- **WHEN** the guard froze a scope itself, and the Budgets action later fires and notifies the guard
- **THEN** the inbox still holds one `freeze` event for that freeze, and one alert was sent

#### Scenario: The first event write was lost
- **WHEN** a freeze was recorded in the guard's state but its event never reached the outbox, and the budget notification is delivered again
- **THEN** the `freeze` event is written with the original freeze's time, and no second alert is sent

### Requirement: A loosening approval describes each change in a sentence
An approval the guard raises for a loosening SHALL have a title naming the kind of change and the payer account in plain words, or saying it loosens spend protection when there are several kinds. It SHALL list each loosening change as a full sentence that names the scope by kind and id and gives money in USD with two decimals, without config field names, arrows, or direction labels.

#### Scenario: A raised ceiling
- **WHEN** version 8 raises account 891376926337's ceiling from 5.30 to 7.00 USD
- **THEN** the title is "Raise a spend cap for payer 339712710400 (config version 8)" and the summary lists "Raise the ceiling of account 891376926337 from 5.30 USD to 7.00 USD."

#### Scenario: A removed scope
- **WHEN** a version removes the scope for OU ou-ab12-cdef3456
- **THEN** the summary lists "Stop protecting OU ou-ab12-cdef3456." and the title names a removed scope

### Requirement: A loosening approval says what it does to a freeze
For each scope a loosening changes that the guard has recorded as frozen, the summary SHALL say the scope is frozen, give its spend this month as read from Cost Explorer, and say whether the change would lift the freeze. When the spend can't be read, the summary SHALL say the scope is frozen and that its spend couldn't be read. A scope that isn't frozen SHALL get no freeze line.

#### Scenario: A raise above spend
- **WHEN** a version raises frozen account 891376926337 to 7.00 USD and its spend is 6.16 USD
- **THEN** the summary says the account is frozen, its spend this month is 6.16 USD, and the change would lift the freeze

#### Scenario: A raise still below spend
- **WHEN** a version raises frozen account 891376926337 to 6.00 USD and its spend is 6.16 USD
- **THEN** the summary says the change would not lift the freeze, because spend is still over the new ceiling

### Requirement: A loosening approval gives exact times
The summary SHALL give the loosening delay in words, the earliest time the change can apply (the time the guard first saw the version plus the delay) in UTC, and the time the request expires in UTC, and SHALL say that if the request is rejected or not answered by then, the stricter config stays in force. The payload SHALL carry the same times as `earliest_apply_at` and `expires_at`, and keep its existing fields.

#### Scenario: A 30-minute delay
- **WHEN** the guard first sees version 8 at 12:55 UTC on 2026-11-12, with a 30-minute delay and a 24-hour window
- **THEN** the summary says the change applies 30 minutes after the guard first saw it, at 13:25 UTC on 2026-11-12 at the earliest, and that the request expires at 12:55 UTC on 2026-11-13

### Requirement: Config refresh alerts give times in words
The reminder sent while the guard can't read its config from Jennah, and the alert sent when it recovers, SHALL give how long reads have failed in words, rounded to the minute, and the time the failures started in UTC in the form "14:45 UTC on 2026-11-12", not in Go duration or RFC 3339 form.

#### Scenario: A six-hour outage reminder
- **WHEN** config reads have failed since 14:45 UTC on 2026-11-12 and the six-hour reminder is sent
- **THEN** the reminder says the guard has been failing since 14:45 UTC on 2026-11-12 (6 hours), and does not contain `6h0m0s`

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
When a reinstalled guard needs approval for a version whose approval an earlier install raised and recorded in the inbox, it SHALL use that existing approval and SHALL NOT ask approvers a second time. When no such record can be found, it SHALL raise a new approval for the version rather than leave the loosening unable to proceed. The loosening delay SHALL be counted from when the new guard first saw the version.

#### Scenario: Approval raised before the reinstall
- **WHEN** version 10's approval was raised at 01:45 UTC, the stack is recreated at 02:20 UTC, and the approval is approved at 02:30 UTC
- **THEN** no second approval request reaches the approvers, and version 10 applies once the loosening delay has passed since 02:20 UTC

#### Scenario: The earlier approval was not recorded
- **WHEN** version 10's approval was raised, but its `loosening_raised` event never reached the inbox, and the stack is recreated
- **THEN** the new guard raises a new approval for version 10, and the config refresh keeps working

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
