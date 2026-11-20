# ai-pack Specification

## Purpose

Caps spend on AI models in Amazon Bedrock and stops abuse of leaked AI credentials, by pricing every Bedrock runtime call from its token counts and cutting off an account's AI access when it passes a cap or trips a rule. Built on the activity guard.

## Requirements

### Requirement: Every Bedrock runtime call is priced
The AI pack SHALL price each successful `InvokeModel`, `InvokeModelWithResponseStream`, `Converse`, and `ConverseStream` call from its input and output token counts and the model's price, taking streaming calls' counts from their completion records. It SHALL match an inference profile id, a model id, and a model ARN for the same model to one price. Prices SHALL come from a table shipped with each release, overridden per model by config. A call refused by AWS SHALL NOT be priced.

#### Scenario: Two ways of naming one model
- **WHEN** one call names `us.amazon.nova-micro-v1:0` and another the inference profile's ARN for the same model
- **THEN** both are priced with that model's input and output prices

#### Scenario: A streaming call
- **WHEN** a `ConverseStream` call completes
- **THEN** its tokens are taken from its completion record and priced once

### Requirement: Caps per account
The AI pack SHALL cut off an account when its estimated AI spend passes the hourly, daily, or monthly cap that config sets for it, directly or through its OU. An account with no AI scope SHALL have no caps, and SHALL still be subject to the tripwires.

#### Scenario: The monthly cap
- **WHEN** an account's estimated AI spend this month passes its monthly cap of $500
- **THEN** the account is cut off and the event names the monthly cap

### Requirement: Tripwires cut off without an estimate
The AI pack SHALL cut off an account at once when it sees a Bedrock call with a model or in a region not on the allowlist that applies to the account, a call made with a Bedrock API key by a caller not allowed to use one, or a change to Bedrock's model invocation logging settings. A call to a model with no price SHALL also trip, unless the payer org's config counts unpriced models and the call can be counted.

#### Scenario: A leaked API key
- **WHEN** a call made with a Bedrock API key comes from an IAM user not listed as allowed
- **THEN** the account is cut off and the event names the user and the API-key use

#### Scenario: An unknown model
- **WHEN** an account calls a model the price tables and config don't price, and config doesn't count unpriced models
- **THEN** the account is cut off and the event names the model

### Requirement: The cutoff denies AI access only
The AI deny policy SHALL deny every Bedrock and AgentCore action that starts paid model work or a new AI spend commitment, through either the `bedrock-runtime` or the `bedrock-mantle` endpoint, and Bedrock API key use, and SHALL deny nothing else: no action outside Bedrock and AgentCore, and no action that only reads, lists, or stops work.

#### Scenario: A cut-off account keeps its other services
- **WHEN** an account is cut off by the AI pack
- **THEN** its Bedrock and AgentCore calls are refused, and its EC2, S3, and other calls are not

#### Scenario: The OpenAI-compatible endpoint
- **WHEN** a cut-off account calls `/v1/chat/completions` on the `bedrock-mantle` endpoint
- **THEN** the call is refused

#### Scenario: Agents, knowledge bases, and batch jobs
- **WHEN** a cut-off account calls `InvokeAgent`, `RetrieveAndGenerate`, or `CreateModelInvocationJob`
- **THEN** each call is refused

#### Scenario: Looking and cleaning up still work
- **WHEN** a cut-off account lists its models, reads a batch job's status, or deletes a provisioned throughput
- **THEN** each call is allowed

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

### Requirement: Accounts already cut off get a widened cutoff in place
When a release widens the AI deny policy, updating the stack SHALL apply the wider policy to accounts already cut off, without a new cutoff, a lift, or a new cutoff id.

#### Scenario: An update during a cutoff
- **WHEN** an account is cut off and the stack is updated to a release whose cutoff also denies mantle inference
- **THEN** the account's mantle calls are refused from then on, and its cutoff id and record are unchanged

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

### Requirement: Each call is priced at its region's price
The AI pack SHALL price a call at its model's price in the region the call was made from, from the release and shared tables merged tighten-only for that region. A model with no price in that region SHALL be priced at its highest price in any region. A config override SHALL apply in every region. A call through a global inference profile SHALL be priced at the regional price.

#### Scenario: A call in London
- **WHEN** an account calls a model in eu-west-2, where it costs $0.0062 per 1,000 input tokens, against $0.004 in us-east-1
- **THEN** the call's input is priced at $0.0062

#### Scenario: No price in the call's region
- **WHEN** an account calls a model in ap-southeast-7, and the tables price it only in us-east-1 at $0.004 and in eu-west-2 at $0.0062
- **THEN** the call's input is priced at $0.0062

#### Scenario: An override
- **WHEN** config overrides a model's price, and an account calls it in sa-east-1
- **THEN** the call is priced at the override

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

### Requirement: Blocked use is denied in enforce scopes
For each account covered by an AI scope in `enforce` mode, the guard SHALL deny the kinds of use blocked for it, within 5 minutes of the config applying, and SHALL stop denying a kind within 5 minutes of an approved change allowing it. Blocking SHALL deny only that kind of use. A `dry-run` scope SHALL block nothing. The guard SHALL record each change to what is blocked where as an inbox event.

#### Scenario: Agents blocked
- **WHEN** account 730335604511's enforce scope blocks `agents`
- **THEN** its `InvokeInlineAgent` calls are refused, and its `Converse` calls still work

#### Scenario: Async invokes blocked
- **WHEN** an account's enforce scope blocks `async_invoke`
- **THEN** its `StartAsyncInvoke` calls are refused, and its `InvokeModel` calls to the same model still work

#### Scenario: A dry-run scope
- **WHEN** an account's scope is in dry-run and blocks `agents`
- **THEN** its agent calls are not refused

#### Scenario: Allowed again
- **WHEN** an approved change removes `agents` from the account's blocked list and the delay has passed
- **THEN** within 5 minutes its agent calls work again

### Requirement: The block policy stays in step and within limits
The guard SHALL keep one block policy, attached to the organization root only while some account has something blocked, listing each blocked account under the kinds blocked for it. When the policy would exceed AWS's size limit, the guard SHALL keep the last policy it wrote and alert once. When the policy can't be attached or updated, the guard SHALL alert once, retry on each pull, and alert when it succeeds.

#### Scenario: Nothing blocked any more
- **WHEN** the last blocked kind is allowed for the last account
- **THEN** the guard detaches the block policy from the root

#### Scenario: Too many accounts
- **WHEN** blocking would list more accounts than fit in a 5,120-character policy
- **THEN** the guard keeps the policy as it was, and alerts once naming how many accounts it couldn't add

### Requirement: A config price below the known price is reported
Once a day while the AI guard is on, the guard SHALL compare each `ai.prices` entry with the model's price from the release table and shared prices, without the provider floor. When the entry is lower on input or output than that price in every region that prices the model, the guard SHALL send one alert and write one `price_override_low` event naming the model, the entry, and the known price. Pricing SHALL NOT change: the entry still applies.

#### Scenario: AWS raises a model's price
- **WHEN** a payer org's `ai.prices` lists `openai.gpt-6.2-sol` at $0.0022 in and $0.011 out, and a shared price version later lists it at $0.0025 in and $0.011 out in every region
- **THEN** the next daily run sends one alert and writes one `price_override_low` event naming the model, $0.0022 in, and $0.0025 in, and calls are still priced at $0.0022

#### Scenario: Cheaper only in some regions
- **WHEN** the entry is $0.0022 in, and the known price is $0.0022 in us-east-1 and $0.0024 in eu-west-2
- **THEN** nothing is reported

#### Scenario: Only the floor is higher
- **WHEN** the entry matches the model's shared manual price, and only the provider floor is higher
- **THEN** nothing is reported

#### Scenario: A model no table prices
- **WHEN** the entry prices a model neither the release table nor the shared prices list
- **THEN** nothing is reported

### Requirement: A low config price is reported once
The guard SHALL NOT report the same entry and known price again. When either changes and the entry is still low, it SHALL report again. When the entry is raised, removed, or no longer low, it SHALL forget the report without an alert.

#### Scenario: The next day
- **WHEN** a low entry was reported and nothing changed
- **THEN** the next daily run sends nothing

#### Scenario: Prices rise again
- **WHEN** a reported entry's known price rises again
- **THEN** the next daily run reports the entry with the new known price

#### Scenario: The org fixes its config
- **WHEN** the org's config raises the entry to the known price, and later a release lowers the known price below the old entry
- **THEN** nothing is reported, since the entry in force is no longer low
