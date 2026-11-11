# Spec Delta

## Purpose

Caps spend on AI models in Amazon Bedrock and stops abuse of leaked AI credentials, by pricing every Bedrock runtime call from its token counts and cutting off an account's AI access when it passes a cap or trips a rule. Built on the activity guard.

## ADDED Requirements

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
The AI pack SHALL cut off an account at once when it sees a Bedrock call with a model or in a region not on the allowlist that applies to the account, a call made with a Bedrock API key by a caller not allowed to use one, or a change to Bedrock's model invocation logging settings. A model the price table doesn't know SHALL be treated as not allowlisted unless config allowlists it and gives its price.

#### Scenario: A leaked API key
- **WHEN** a call made with a Bedrock API key comes from an IAM user not listed as allowed
- **THEN** the account is cut off and the event names the user and the API-key use

#### Scenario: An unknown model
- **WHEN** an account calls a model the price table doesn't list and config doesn't allowlist
- **THEN** the account is cut off and the event names the model

### Requirement: The cutoff denies AI access only
The AI deny policy SHALL deny Bedrock model invocation (`bedrock:InvokeModel` and `bedrock:InvokeModelWithResponseStream`, which also govern `Converse`), Bedrock API key use (`bedrock:CallWithBearerToken`), and AgentCore invoke and session actions, and SHALL deny nothing else.

#### Scenario: A cut-off account keeps its other services
- **WHEN** an account is cut off by the AI pack
- **THEN** its Bedrock and AgentCore calls are refused, and its EC2, S3, and other calls are not
