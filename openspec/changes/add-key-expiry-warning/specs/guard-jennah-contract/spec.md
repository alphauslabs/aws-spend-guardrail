# Spec Delta

## ADDED Requirements

### Requirement: The guard reports its keys' expiry
Once a day, the guard SHALL read the expiry of its reader and writer keys from Jennah, using each key to describe itself, and SHALL record for each key in that day's heartbeat event its label and either its expiry, `never`, or `unknown`. A failure to read a key's expiry SHALL NOT stop the daily run.

#### Scenario: Both keys expire
- **WHEN** the reader expires on 2027-11-11 and the writer on 2027-11-12
- **THEN** the day's heartbeat records both labels and both expiry times

#### Scenario: Jennah doesn't report the key
- **WHEN** Jennah answers without the calling key's metadata, or the call fails
- **THEN** the heartbeat records that key's expiry as `unknown`, and the rest of the daily run proceeds

### Requirement: The guard warns before a key expires
The guard SHALL publish a local alert when a key's expiry is 30, 14, 7, 3, or 1 days away, once per threshold per key, naming which key (reader or writer), its label, the expiry, and that it must be rotated. A key with no expiry or an unknown expiry SHALL raise no warning. When a key's id or expiry changes, its warnings SHALL start over.

#### Scenario: The first warning
- **WHEN** the daily run finds the reader expires in 29 days
- **THEN** one alert says the reader key expires on that date and must be rotated, and the next day's run sends nothing new

#### Scenario: Thresholds passed while the guard wasn't running
- **WHEN** the daily run finds a key expires in 6 days and no warning was sent for it before
- **THEN** one alert is sent for the 7-day threshold, not one per missed threshold

#### Scenario: A rotated key
- **WHEN** the reader was warned at 14 days and is then replaced by a key that expires in a year
- **THEN** no alert is sent, and the new key is warned 30 days before its own expiry

#### Scenario: A key that never expires
- **WHEN** the writer key has no expiry
- **THEN** no warning is ever sent for it, and the heartbeat records its expiry as `never`
