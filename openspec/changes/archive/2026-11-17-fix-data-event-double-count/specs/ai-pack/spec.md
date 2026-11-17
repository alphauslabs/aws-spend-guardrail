## ADDED Requirements

### Requirement: A call the trail logs twice is counted once
When the trail records a Bedrock call as both a management event and a data event, the AI pack SHALL price it, count it, and check it against the tripwires once, from the management event. It SHALL do so without keeping state across trail files.

#### Scenario: A trail that logs model data events
- **WHEN** the trail logs `AWS::Bedrock::Model` data events, and an account makes one `Converse` call with 3 input and 20 output tokens
- **THEN** the account's totals grow by one call, 3 input tokens, and 20 output tokens

#### Scenario: A tripwire on such a trail
- **WHEN** the trail logs `AWS::Bedrock::Model` data events, and an account calls a model not on its allowlist
- **THEN** the account is cut off once, and one event is written

#### Scenario: A trail without data events
- **WHEN** the trail logs only management events
- **THEN** each call is counted once, as before
