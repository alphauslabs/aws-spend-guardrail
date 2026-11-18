## ADDED Requirements

### Requirement: Mantle usage is priced and checked
The AI pack SHALL price mantle usage from its input and output tokens and the model's price, from the same tables and overrides as runtime calls, at the price in the region it was read from, and count it against the same caps. An allowlist entry SHALL allow a mantle model id that differs from it only by a version suffix. Mantle usage SHALL be checked against the model and region allowlists and the unknown-model policy; the API-key tripwire SHALL NOT apply, as the usage names no caller.

#### Scenario: A priced model
- **WHEN** an account's mantle usage for `openai.gpt-oss-20b` in eu-west-2 in one minute is 140 input and 117 output tokens, in 2 inferences
- **THEN** the usage is priced at `openai.gpt-oss-20b`'s eu-west-2 price and added to the account's totals as 2 calls

#### Scenario: Allowlisted by its runtime id
- **WHEN** an account's allowlist names `openai.gpt-oss-20b-1:0`, and it has mantle usage for `openai.gpt-oss-20b`
- **THEN** the usage is allowed

#### Scenario: A model not on the allowlist
- **WHEN** an account's allowlist names only `anthropic.claude-haiku-5-5`, and it has mantle usage for `openai.gpt-6-sol`
- **THEN** the account is cut off and the event names the model and that it came through mantle

#### Scenario: An unpriced model
- **WHEN** an account has mantle usage for a model no table prices, and config doesn't count unpriced models
- **THEN** the account is cut off as for a runtime call to that model
