# activity-guard Specification

## Purpose

Cuts off fast-moving spend from the management account, within minutes, by reading the organization CloudTrail trail as it lands, pricing what each pack recognizes, keeping per-account totals, and attaching a pack's narrow deny policy. Shared by every pack; the AI pack is the first.

## Requirements

### Requirement: The guard reads the organization trail
When a pack is enabled, the guard SHALL read every new log file the organization trail writes to its bucket, from every account and region in the organization, without anything installed in member accounts. It SHALL read each file once, including after a restart, and SHALL work from a bucket in the management account or in another account that grants it read access, and with files encrypted by a KMS key the guard is allowed to decrypt with.

#### Scenario: A call in a member account
- **WHEN** a member account makes a Bedrock call and the trail writes the file holding it
- **THEN** the guard reads that record within about a minute of the file landing

#### Scenario: Encrypted files in a log archive account
- **WHEN** the trail's bucket and KMS key are in another account whose bucket and key policies grant the guard's role read and decrypt
- **THEN** the guard reads the files as it would from its own account

#### Scenario: Access is lost
- **WHEN** the key policy stops allowing the guard to decrypt
- **THEN** within 30 minutes the stalled-trail alert names the access error

#### Scenario: A restart
- **WHEN** the guard's function restarts while files are arriving
- **THEN** no file is read twice and none is skipped

### Requirement: Totals are kept per account over rolling windows
The guard SHALL keep, for each account and pack, the estimated spend over the last hour, the current UTC day, and the current calendar month, and SHALL count a call at the time CloudTrail recorded it, not when the file was read.

#### Scenario: A late file
- **WHEN** a file holding calls from 10:58 lands at 11:03
- **THEN** those calls count toward the hour ending at 11:00 and later windows that include 10:58

### Requirement: A pack cuts off an account with its own deny policy
When a pack's cap is passed or one of its tripwires fires, the guard SHALL attach that pack's deny policy to the account in `enforce` mode, record the cutoff with its evidence, and alert locally. In `dry-run` mode it SHALL record and alert what it would have cut off, and attach nothing. The policy SHALL deny only what the pack covers. An account already cut off by the pack SHALL NOT be cut off again.

#### Scenario: A cap is passed in enforce mode
- **WHEN** an account's estimated hourly AI spend passes its hourly cap
- **THEN** the AI deny policy is attached to that account, a cutoff event records the cap, the total, and the top callers and models, and a local alert is sent

#### Scenario: Dry-run
- **WHEN** the same happens in dry-run mode
- **THEN** a would-cut-off event and an alert are recorded, and no policy is attached

### Requirement: A cutoff is lifted only by an approved loosening
A cutoff SHALL stay until a config version that lifts it, or raises the cap it passed, is approved and past the stack's delay. A lifted cutoff SHALL be detached and recorded. When a lift is approved but the account still breaks a cap or tripwire, the guard SHALL lift it and cut it off again only on a new breach, recording both.

#### Scenario: A lift is approved
- **WHEN** a version listing a cutoff under lifts is approved and the delay has passed
- **THEN** the deny policy is detached from the account and a lift event is recorded

### Requirement: The guard reports a stalled trail
When no new trail file has arrived for 30 minutes, the guard SHALL alert locally and record it, and SHALL alert again when files resume. Totals SHALL NOT be reset by a stall.

#### Scenario: The trail stops
- **WHEN** the trail is stopped or its bucket stops receiving files
- **THEN** within 30 minutes an alert says the trail is stalled, since when, and that AI spend is not being watched

### Requirement: A trail file counts once its spend is stored
The guard SHALL treat a trail file as read only once the spend of every priced call in it is stored. When storing fails, the guard SHALL read the same files again on the next run, SHALL alert locally once that AI spend is not being counted, and SHALL alert again when storing works. A call MAY be counted twice after a partial failure, but SHALL never be dropped.

#### Scenario: The state table refuses writes
- **WHEN** the guard reads a file of priced calls and can't store their spend
- **THEN** it alerts once that AI spend is not counted, keeps reading that file on every run, and once storing works counts the calls, checks caps against them, and alerts that spend is counted again

### Requirement: The fast path never waits on Jennah
Reading the trail, keeping totals, and cutting off SHALL work while Jennah is unreachable, on the last valid config. Events SHALL reach the inbox when Jennah returns.

#### Scenario: Jennah is down during a breach
- **WHEN** an account passes its cap while Jennah is unreachable
- **THEN** the account is cut off on time and the cutoff event arrives in the inbox after Jennah returns
