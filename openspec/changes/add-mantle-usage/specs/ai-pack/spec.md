## ADDED Requirements

### Requirement: Mantle usage is priced and checked
The AI pack SHALL price mantle usage from its input and output tokens and the model's price, from the same tables and overrides as runtime calls, and count it against the same caps. A mantle model id SHALL match a table model that differs from it only by a version suffix. Mantle usage SHALL be checked against the model and region allowlists and the unknown-model policy; the API-key tripwire SHALL NOT apply, as the usage names no caller.

#### Scenario: A priced model
- **WHEN** an account's mantle usage for `openai.gpt-oss-20b` in one minute is 140 input and 117 output tokens, and the table prices `openai.gpt-oss-20b-1:0`
- **THEN** the usage is priced with that model's prices and added to the account's totals as 2 calls if the minute had 2 inferences

#### Scenario: A model not on the allowlist
- **WHEN** an account's allowlist names only `anthropic.claude-haiku-5-5`, and it has mantle usage for `openai.gpt-6-sol`
- **THEN** the account is cut off and the event names the model and that it came through mantle

#### Scenario: An unpriced model
- **WHEN** an account has mantle usage for a model no table prices, and config doesn't count unpriced models
- **THEN** the account is cut off as for a runtime call to that model
