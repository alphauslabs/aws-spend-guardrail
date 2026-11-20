# Spec Delta

## ADDED Requirements

### Requirement: Events don't wait on guard tasks
Writing queued events to the inbox SHALL NOT share the guard's one-at-a-time task slot. While Jennah is reachable, a queued event SHALL be written within a few minutes, however often the guard's pull, trail, daily, and other tasks run.

#### Scenario: The AI guard is on
- **WHEN** the trail task runs every minute and a pull applies a version
- **THEN** the `config_applied` event is in the inbox within 5 minutes

### Requirement: An event already in the inbox counts as delivered
When Jennah refuses an event's write as invalid, the guard SHALL check the inbox for the event's id. If the id is there, the queued event SHALL count as delivered and leave the queue, and the inbox SHALL keep the event first written under that id. If it isn't, the write SHALL be retried as for any other failure.

#### Scenario: The same id with different content
- **WHEN** an event is queued whose id the inbox already holds with a different time or detail
- **THEN** it leaves the queue on its next delivery attempt, the inbox keeps the first event, and nothing reaches the dead-letter queue

#### Scenario: An invalid event never written
- **WHEN** Jennah refuses an event as invalid and the inbox has no event with its id
- **THEN** it stays queued and is retried, and reaches the dead-letter queue and its alert if it never succeeds

### Requirement: Failed event writes are logged
Each failed attempt to write a queued event SHALL be logged in the payer account with the event's id and kind and the error, without the event's detail.

#### Scenario: Jennah is unreachable
- **WHEN** a write fails because Jennah can't be reached
- **THEN** the guard's outbox log has a line naming the event id, its kind, and the error

### Requirement: Each daily run writes its own heartbeat
The daily heartbeat's id SHALL be derived from its date and its content, so a second daily run on the same day with a different state writes its own heartbeat, while a retried write of the same heartbeat stays one event.

#### Scenario: A second daily run
- **WHEN** the daily task runs again on the same day after the state changed
- **THEN** the inbox holds both daily heartbeats, and no event is left undeliverable
