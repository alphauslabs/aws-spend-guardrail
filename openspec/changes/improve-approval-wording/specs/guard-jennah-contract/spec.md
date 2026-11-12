## ADDED Requirements

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
