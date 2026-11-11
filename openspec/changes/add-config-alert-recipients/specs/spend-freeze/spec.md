# Spec Delta

## ADDED Requirements

### Requirement: Local alerts reach the config's recipients
The guard SHALL subscribe each recipient in force that has no subscription on the local alert topic, and SHALL unsubscribe each confirmed subscription it made whose address is no longer a recipient. It SHALL record each address it subscribes or unsubscribes in the inbox.

#### Scenario: A recipient is added
- **WHEN** an approved version adds `finops@example.com`
- **THEN** on that run the guard subscribes it, `finops@example.com` receives one confirmation email, and an event records the subscription

#### Scenario: A recipient is removed
- **WHEN** an approved version removes `finops@example.com`, whose subscription the guard made
- **THEN** the guard unsubscribes it, it receives no later alert, and an event records the removal

#### Scenario: Jennah is unreachable
- **WHEN** config refresh from Jennah fails
- **THEN** the recipients in force keep their subscriptions and receive the outage alert

### Requirement: Unconfirmed recipients aren't flooded
While a subscription the guard asked for awaits confirmation, the guard SHALL ask SNS again for that address at most once a day, and SHALL record the address as awaiting confirmation in the inbox once a day.

#### Scenario: Nobody confirms
- **WHEN** `finops@example.com` has not confirmed for two days
- **THEN** it has received at most two confirmation emails, and the inbox records it as awaiting confirmation

### Requirement: Config can't remove the stack's recipient or outside subscriptions
The guard SHALL NOT unsubscribe the address given in the stack's `AlertEmail`, or any subscription it did not make, whatever the config lists. The guard's role SHALL be allowed to subscribe, unsubscribe, and list subscriptions on the local alert topic only.

#### Scenario: Config lists nobody
- **WHEN** the version in force has no `alert_emails` and the stack's `AlertEmail` is `oncall@example.com`
- **THEN** `oncall@example.com` stays subscribed and receives every alert

#### Scenario: Config lists the stack's address, then drops it
- **WHEN** an approved version removes `oncall@example.com`, which is also the stack's `AlertEmail`
- **THEN** it stays subscribed

#### Scenario: A subscription added by hand
- **WHEN** someone subscribed `noc@example.com` to the alert topic in the console, and no version lists it
- **THEN** the guard leaves it subscribed
