## ADDED Requirements

### Requirement: An AI scope can cap callers
An AI scope SHALL accept an optional `callers` list. Each entry SHALL name exactly one of `role`, an IAM role name, or `user`, an IAM user name, and SHALL set at least one of `hourly_usd`, `daily_usd`, and `monthly_usd`. A caller entry SHALL take its mode from its scope and inherit no caps from `ai.defaults`.

#### Scenario: A role and an API key's user
- **WHEN** an account scope lists `{"role": "app-chatbot", "daily_usd": 20}` and `{"user": "BedrockAPIKey_ab12", "monthly_usd": 100}`
- **THEN** it validates, and each caller has only the cap it sets

### Requirement: Caller entries are validated
Validation SHALL refuse a caller entry that names both or neither of `role` and `user`, a name that isn't a valid IAM name, a full ARN, an unknown field, a negative cap, an entry with no cap, a caller named twice in one scope, and a caller cap higher than its scope's cap for the same window.

#### Scenario: A caller cap above the scope cap
- **WHEN** a scope has a daily cap of $50 and lists a role with a daily cap of $80
- **THEN** validation fails naming the caller's `daily_usd` and the scope's cap

#### Scenario: An ARN instead of a name
- **WHEN** a caller entry sets `role` to `arn:aws:iam::111122223333:role/app-chatbot`
- **THEN** validation fails and says to give the role's name

### Requirement: Caller cap changes are classified
A new caller entry, a new or lower caller cap, and a caller cutoff id added to `ai.lifts` SHALL be classified as the existing AI rules say for scopes and lifts: a new entry or a new or lower cap SHALL be tightening and apply at once; a higher or removed caller cap, or a removed caller entry, SHALL be loosening and need the approval and delay. A loosening approval SHALL name the caller and the scope.

#### Scenario: Raising a role's cap
- **WHEN** a version raises role `app-chatbot`'s daily cap in account 111122223333 from $20 to $40
- **THEN** it waits for approval, and the approval names the role, the account, and both caps

#### Scenario: Adding a caller
- **WHEN** a version adds a caller entry for role `app-chatbot` with a daily cap of $20
- **THEN** the guard applies it on its next pull, with no approval

## MODIFIED Requirements

### Requirement: The AI section is validated
Validation SHALL refuse an unknown field in the `ai` section, a negative cap, a scope that names an account or OU twice, a price override with a missing or negative price, a lift naming no cutoff id, and an `unknown_models` value other than `cutoff` or `count`. A lift SHALL name an account cutoff id like `ai:111122223333:20261111T0930` or a caller cutoff id like `ai:111122223333:20261111T0930:role/app-chatbot`.

#### Scenario: An invalid AI section
- **WHEN** a document gives a scope a negative hourly cap
- **THEN** validation fails naming the field

#### Scenario: An invalid unknown-models value
- **WHEN** a document sets `ai.unknown_models` to `allow`
- **THEN** validation fails naming the field and the two values it accepts

#### Scenario: Lifting a caller cutoff
- **WHEN** a document lists `ai:111122223333:20261120T0930:role/app-chatbot` under `ai.lifts`
- **THEN** it validates
