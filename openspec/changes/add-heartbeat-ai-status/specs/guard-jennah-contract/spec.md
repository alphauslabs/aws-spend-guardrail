## MODIFIED Requirements

### Requirement: The heartbeat carries the AI guard's state when it is on
When the AI guard is on, each heartbeat SHALL give its mode, the accounts cut off with their rule and since when, whether the trail is stalled, how unpriced models are treated, the accounts and kinds the block policy denies and whether it is attached, and each watched mantle region with whether its usage can be read and the accounts not linked in it. When it is off, the heartbeat SHALL have no AI section.

#### Scenario: No AI guard
- **WHEN** the AI guard is off
- **THEN** the heartbeat has no AI section

#### Scenario: Blocks and mantle
- **WHEN** the AI guard blocks agents on account 730335604511, watches mantle in us-east-1, and can't read us-west-2
- **THEN** the heartbeat's AI section lists 730335604511 with `agents` under the blocks, us-east-1 as read, and us-west-2 as not read with the error

### Requirement: The heartbeat is written when the guard's state changes
Besides the daily heartbeat, the guard SHALL write a heartbeat at the end of any run in which its state changed since the last heartbeat it wrote: a scope frozen or lifted, an account cut off or its cutoff lifted, a version applied, a loosening raised or closed, the trail stalling or resuming, the set of unprotected accounts changing, what the block policy denies changing, or a mantle region becoming unreadable or readable again.

#### Scenario: A freeze between daily runs
- **WHEN** the guard freezes a scope at 14:20 UTC
- **THEN** a heartbeat written in that run lists the scope as frozen, and a reader taking the newest heartbeat sees it without waiting for the next day

#### Scenario: A mantle region stops being read
- **WHEN** the guard can't read mantle usage in us-east-1 on a run where it could before
- **THEN** a heartbeat written in that run shows us-east-1 as not read
