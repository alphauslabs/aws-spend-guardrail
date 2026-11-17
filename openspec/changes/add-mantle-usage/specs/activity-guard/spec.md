## ADDED Requirements

### Requirement: The guard reads mantle usage from shared metrics
When a pack is enabled and the stack lists mantle regions, the guard SHALL read, once a minute in each listed region, the `bedrock-mantle` input and output tokens and inference counts per account and model that member accounts share with the payer account through a CloudWatch cross-account observability sink. It SHALL count each usage at the minute of its datapoint, and nothing SHALL be installed in member accounts beyond the link.

#### Scenario: A mantle call in a member account
- **WHEN** a linked member account makes a Chat Completions call on the `bedrock-mantle` endpoint in a listed region
- **THEN** within about two minutes the guard counts its tokens toward that account's totals, at the minute of the call

#### Scenario: No mantle regions listed
- **WHEN** the stack lists no mantle regions
- **THEN** the guard reads no metrics, and the trail is read as before

### Requirement: A minute's mantle usage counts once
The guard SHALL count each account, model, and minute's tokens and calls once. When a datapoint grows after the guard read it, the guard SHALL count only the growth. It SHALL keep what it has counted until usage is stored, as for trail files, and SHALL stop revisiting a minute 15 minutes after it.

#### Scenario: A datapoint grows
- **WHEN** the guard reads 12 input tokens for a minute, and a later read of the same minute shows 30
- **THEN** 18 more tokens are counted, and the minute's total is 30

#### Scenario: Usage can't be stored
- **WHEN** the guard reads mantle usage and can't store it
- **THEN** it reads the same minutes again on the next run, and counts them once storing works

### Requirement: The guard reports mantle metrics it can't read
When the guard can't read a listed region's mantle metrics, it SHALL alert locally once that mantle spend in that region is not being watched, naming the error, and SHALL alert again when reading works. Totals SHALL NOT be reset.

#### Scenario: The sink is deleted
- **WHEN** the payer account's sink in a listed region is deleted
- **THEN** within a few minutes one alert says mantle spend in that region is not watched, and no further alert is sent until reading works again

### Requirement: Accounts not sharing mantle metrics are reported once
At least daily, for each listed region, the guard SHALL find the active member accounts not linked to the payer account's sink, and SHALL write a `mantle_unlinked` event and alert locally the first time it finds an account so. It SHALL NOT report the account again until it has been linked and then unlinked again.

#### Scenario: A new account without a link
- **WHEN** an account joins the organization and isn't linked to the sink in `us-east-1`, a listed region
- **THEN** the next daily check writes one `mantle_unlinked` event naming the account and region, and alerts once
