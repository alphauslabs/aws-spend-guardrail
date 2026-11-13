## ADDED Requirements

### Requirement: The heartbeat lists the scopes in force and what is frozen
Each heartbeat event SHALL list every scope in force with its target, mode, ceiling, threshold, period, freeze style, and Budgets' actual spend when known, and SHALL give, for each frozen scope, since when and how it was frozen. It SHALL carry a `state_layout` number that changes when the shape of the state changes.

#### Scenario: A frozen scope
- **WHEN** account 891376926337 has been frozen since 03:02 UTC by the guard's own attach
- **THEN** the next heartbeat lists that scope as frozen since 03:02 UTC, with how it was frozen, beside its ceiling and threshold

### Requirement: The heartbeat names the pending loosening and the accounts left out
Each heartbeat SHALL name the pending loosening, if any, with its version, approval id, and earliest apply time, and SHALL list the excluded accounts and the accounts reported as unprotected.

#### Scenario: A pending loosening
- **WHEN** version 11 is waiting for approval `98319342`
- **THEN** the heartbeat names version 11, that approval id, and the earliest time it can apply

### Requirement: The heartbeat carries the AI guard's state when it is on
When the AI guard is on, each heartbeat SHALL give its mode, the accounts cut off with their rule and since when, and whether the trail is stalled. When it is off, the heartbeat SHALL have no AI section.

#### Scenario: No AI guard
- **WHEN** the AI guard is off
- **THEN** the heartbeat has no AI section

### Requirement: The heartbeat is written when the guard's state changes
Besides the daily heartbeat, the guard SHALL write a heartbeat at the end of any run in which its state changed since the last heartbeat it wrote: a scope frozen or lifted, an account cut off or its cutoff lifted, a version applied, a loosening raised or closed, the trail stalling or resuming, or the set of unprotected accounts changing.

#### Scenario: A freeze between daily runs
- **WHEN** the guard freezes a scope at 14:20 UTC
- **THEN** a heartbeat written in that run lists the scope as frozen, and a reader taking the newest heartbeat sees it without waiting for the next day

### Requirement: Spend alone doesn't write a heartbeat, and the newest one is the latest state
Spend figures moving SHALL NOT count as a change. Each state-change heartbeat SHALL have its own id, and the daily heartbeat SHALL keep its id of one per day, so the newest heartbeat is always the guard's latest state.

#### Scenario: Nothing changed
- **WHEN** a run changes nothing but the spend figures
- **THEN** no heartbeat is written in that run

#### Scenario: The daily heartbeat
- **WHEN** the daily run writes its heartbeat on a day with earlier state-change heartbeats
- **THEN** it is recorded as that day's heartbeat, with the keys' expiry and the state, and the earlier ones stay
