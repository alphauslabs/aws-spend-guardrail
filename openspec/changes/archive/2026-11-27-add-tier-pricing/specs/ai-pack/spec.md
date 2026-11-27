## MODIFIED Requirements

### Requirement: Every Bedrock runtime call is priced
The AI pack SHALL price each successful `InvokeModel`, `InvokeModelWithResponseStream`, `Converse`, `ConverseStream`, `ChatCompletions`, and `Responses` call from its token counts and the model's price, streaming calls from the records that carry the counts. An inference profile id, model id, and model ARN for one model SHALL get one price, from the release's table, overridden per model by config. A call refused by AWS SHALL NOT be priced.

#### Scenario: Two ways of naming one model
- **WHEN** one call names `us.amazon.nova-micro-v1:0` and another the inference profile's ARN for the same model
- **THEN** both are priced with that model's input and output prices

#### Scenario: A streaming call
- **WHEN** a `ConverseStream` call completes
- **THEN** its tokens are taken from its completion record and priced once

#### Scenario: A call through the OpenAI-compatible API
- **WHEN** an account calls `/openai/v1/chat/completions` on `bedrock-runtime` with `openai.gpt-oss-20b-1:0`, and the `ChatCompletions` record carries 69 input and 5 output tokens
- **THEN** the call is priced at the model's price for its tier and counted against the account's caps, allowlists, and unknown-model policy like an `InvokeModel` call

## ADDED Requirements

### Requirement: Each call is priced at the service tier it used
The AI pack SHALL price a call whose record names a service tier at its model's price for that tier, in the call's region, from the release and shared tables merged tighten-only. A call whose record can carry a tier but names none SHALL be priced at the standard price. A Flex call SHALL be priced at the Flex price.

#### Scenario: A Priority call
- **WHEN** a `Converse` call to `openai.gpt-oss-20b-1:0` in us-east-1 records `serviceTier` `priority`, and the tables price the model's Priority tier above its standard tier
- **THEN** the call's tokens are priced at the Priority price

#### Scenario: No tier named
- **WHEN** a `Converse` call records no `serviceTier`
- **THEN** the call is priced at the model's standard price, as before

#### Scenario: A Flex call
- **WHEN** a `Converse` call records `serviceTier` `flex`, and the tables price the model's Flex tier at half its standard price
- **THEN** the call is priced at the Flex price

### Requirement: A call whose tier can't be told is priced at the highest tier
Usage whose record can't carry a service tier, such as mantle usage read from metrics, SHALL be priced at the highest price of any tier its model has in that region, input and output separately. A model with no tier prices SHALL be priced at its standard price. A `ConverseStream` completion SHALL take the tier of the `ConverseStream` call record it belongs to, and SHALL be priced this way when the guard hasn't seen that record.

#### Scenario: Mantle usage of a model with an Ultrafast tier
- **WHEN** an account's mantle usage is for a model whose standard price is $0.011 input and $0.055 output and whose Ultrafast price is $0.066 and $0.33 per 1,000 tokens
- **THEN** the usage is priced at $0.066 input and $0.33 output

#### Scenario: A model with only a standard price
- **WHEN** an account's mantle usage is for a model with no tier prices
- **THEN** the usage is priced at the model's standard price, as before

#### Scenario: A Priority stream
- **WHEN** a `ConverseStream` call records `serviceTier` `priority`, and its completion record, which has the token counts and no tier, names the call's request id
- **THEN** the completion's tokens are priced at the Priority price

#### Scenario: A stream whose call record wasn't seen
- **WHEN** a `ConverseStream` completion names a request id the guard has no call record for, and its model has an Ultrafast tier
- **THEN** the completion is priced at the model's highest tier price

### Requirement: A call with no token counts trips unless allowed
A successful call whose records carry no token counts, such as a streamed `ChatCompletions` call, SHALL trip `unmeasured_call`, naming the model and the caller, unless config sets `ai.unmeasured_calls` to `allow`. When allowed, it SHALL count as one call at $0, and the first such call per account and model SHALL send one alert and write one `unmeasured_call` event.

#### Scenario: A streamed Chat Completions call
- **WHEN** an account in an enforce scope makes a streamed `/openai/v1/chat/completions` call, and its completion record has `stream: true` and no token counts
- **THEN** the account is cut off, and the cutoff event names the model, the caller, and the `unmeasured_calls` setting that would allow it

#### Scenario: Unmeasured calls allowed
- **WHEN** config sets `ai.unmeasured_calls` to `allow`, and an account makes 50 streamed Chat Completions calls
- **THEN** the account isn't cut off, its totals gain 50 calls and $0, and one alert and one `unmeasured_call` event are sent

#### Scenario: A Responses call without token counts
- **WHEN** a successful non-streamed `Responses` call record carries no token counts
- **THEN** it is treated as unmeasured, as a streamed Chat Completions call is

#### Scenario: A streamed InvokeModel call
- **WHEN** an `InvokeModelWithResponseStream` completion record carries token counts
- **THEN** it is priced from them, as before

### Requirement: An unknown tier is priced high and reported once
A call whose record names a tier the tables don't price for its model SHALL be priced at the model's highest tier price, or its standard price when it has no tier prices. The first such call per model and tier SHALL send one alert and write one `unpriced_tier` event naming the model, the tier, the account, and the price used.

#### Scenario: A tier AWS adds later
- **WHEN** a `Converse` call records `serviceTier` `turbo`, which no table prices for its model
- **THEN** the call is priced at the model's highest tier price, and one alert and one `unpriced_tier` event are sent

#### Scenario: Later calls on the same tier
- **WHEN** 200 more calls to that model record `turbo`
- **THEN** no further alert or event is sent

### Requirement: A config price sets the standard tier
An `ai.prices` entry SHALL be a model's standard price in every region. A call on another tier SHALL be priced at the higher of the entry and the tables' price for that tier, input and output separately, so an entry never prices a premium tier below what the tables know.

#### Scenario: A premium call with an override
- **WHEN** config prices a model at $0.002 input and $0.01 output, the tables price its Priority tier at $0.004 and $0.02, and a call records `priority`
- **THEN** the call is priced at $0.004 input and $0.02 output

#### Scenario: A standard call with an override
- **WHEN** the same model is called with no tier
- **THEN** the call is priced at the override

### Requirement: Ultrafast use the trail didn't show is reported
Once a day, in each region the stack lists for mantle, the guard SHALL read the day's Ultrafast tokens per account and model from Bedrock runtime's metrics. For each account and model with Ultrafast tokens that the trail recorded no Ultrafast call for that day, it SHALL send one alert and write one `ultrafast_unseen` event naming them and the tokens.

#### Scenario: Ultrafast calls recorded another way
- **WHEN** an account's metrics show 40,000 Ultrafast output tokens for `openai.gpt-6-astra` today, and the trail showed it no Ultrafast call today
- **THEN** one alert and one `ultrafast_unseen` event name the account, the model, and the tokens

#### Scenario: Ultrafast calls the trail showed
- **WHEN** the trail recorded the account's Ultrafast calls to that model today
- **THEN** nothing is reported
