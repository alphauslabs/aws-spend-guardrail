## ADDED Requirements

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
