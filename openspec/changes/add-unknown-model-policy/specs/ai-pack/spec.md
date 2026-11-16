## MODIFIED Requirements

### Requirement: Tripwires cut off without an estimate
The AI pack SHALL cut off an account at once when it sees a Bedrock call with a model or in a region not on the allowlist that applies to the account, a call made with a Bedrock API key by a caller not allowed to use one, or a change to Bedrock's model invocation logging settings. A call to a model with no price SHALL also trip, unless the payer org's config counts unpriced models and the call can be counted.

#### Scenario: A leaked API key
- **WHEN** a call made with a Bedrock API key comes from an IAM user not listed as allowed
- **THEN** the account is cut off and the event names the user and the API-key use

#### Scenario: An unknown model
- **WHEN** an account calls a model the price tables and config don't price, and config doesn't count unpriced models
- **THEN** the account is cut off and the event names the model

## ADDED Requirements

### Requirement: Unpriced models can be counted at the highest known price
When config counts unpriced models, the AI pack SHALL price a call to an allowed model that has no price at the highest input price and the highest output price it knows, from the release table, the shared table, and the org's price overrides. The call SHALL count against the account's caps like any other. A call to an unpriced model whose record has no token counts SHALL still trip.

#### Scenario: A new model is counted, not cut off
- **WHEN** config counts unpriced models, and an account with a $50 daily cap calls `xai.grok-4.6`, which has no price, with 1,000 input and 1,000 output tokens, and the highest prices known are $0.015 and $0.075 per 1,000
- **THEN** the account is not cut off, and $0.09 is added to its totals

#### Scenario: The estimate passes a cap
- **WHEN** counted calls to an unpriced model take an account past its hourly cap
- **THEN** the account is cut off by the hourly cap, as for a priced model

#### Scenario: No token counts
- **WHEN** config counts unpriced models and a call to an unpriced model has no input or output token counts
- **THEN** the account trips `unknown_model`, as without counting

#### Scenario: An allowlist still applies
- **WHEN** config counts unpriced models and lists allowed models, and an account calls an unpriced model that isn't listed
- **THEN** the account trips `model_not_allowed`

### Requirement: Each counted unpriced model is reported once
The first time the guard counts a call to an unpriced model, it SHALL send one alert and write one `unpriced_model` event naming the model, the account, and the prices used, and saying how to price the model. Later calls to that model SHALL NOT repeat them, unless the model was priced in the meantime and then lost its price.

#### Scenario: Many calls to one new model
- **WHEN** three accounts make 500 calls to `xai.grok-4.6` while it has no price
- **THEN** one alert and one `unpriced_model` event are sent, naming the first account
