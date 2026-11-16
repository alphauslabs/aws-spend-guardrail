# shared-prices Specification

## Purpose

Keeps the guard's price tables in Jennah, published once and read by every guard, so new models and higher prices reach every payer org without a release, while a shared table can never lower a price or otherwise let more spend through on its own.

## Requirements

### Requirement: Prices are published as versions of chunks
A price version SHALL be one manifest row listing its chunks, each chunk identified by pack and region and carrying a hash of its content, and one chunk row per pack and region holding that chunk's prices. A version, once published, SHALL NOT change. A chunk SHALL NOT exceed 1 MiB, and publishing SHALL refuse a version with a larger chunk.

#### Scenario: Publishing a version
- **WHEN** an operator publishes prices with one AI chunk for us-east-1
- **THEN** the dataset gains a new version whose manifest lists that chunk and its hash, and the chunk can be read by its version, pack, and region

#### Scenario: An oversized chunk
- **WHEN** a chunk would exceed 1 MiB
- **THEN** publishing fails naming the chunk, and no version is written

### Requirement: Guards fetch only what changed
On each pull, the guard SHALL read the latest manifest, and SHALL download a chunk only when it runs that pack, needs that region, and the chunk's hash differs from the one it holds. A chunk whose content doesn't match its manifest hash, or doesn't parse, SHALL be ignored and alerted, and the guard SHALL keep its previous prices.

#### Scenario: Nothing changed
- **WHEN** the latest manifest's chunk hashes match what the guard holds
- **THEN** the guard downloads no chunk

#### Scenario: A corrupt chunk
- **WHEN** a chunk's content doesn't match its hash
- **THEN** the guard keeps its previous prices for that chunk and alerts once

### Requirement: Shared prices can only tighten
The guard SHALL price a model the release table knows at the higher of the release price and the shared price, separately for input and output. A model only the shared table lists SHALL be priced at no less than, separately for input and output, the highest price of any model from the same provider in the release table, or of any model in the release table when the provider has none there. A payer org's `ai.prices` override SHALL still take precedence, as it is approved per org.

#### Scenario: A higher shared price
- **WHEN** the release prices a model at $0.003 per 1,000 input tokens and the shared table at $0.004
- **THEN** the guard prices input at $0.004

#### Scenario: A lower shared price
- **WHEN** the shared table lists a model at half its release price
- **THEN** the guard keeps the release price

#### Scenario: A new model
- **WHEN** the shared table lists `anthropic.claude-new-model` at $0.001 per 1,000 input tokens and the release table's most expensive Anthropic model costs $0.0055
- **THEN** the guard prices the new model's input at $0.0055, and it no longer trips as unknown

### Requirement: Prices keep working without Jennah
When Jennah is unreachable, the prices dataset is missing, or the guard's key can't read it, the guard SHALL keep its last good shared prices, or the release table alone if it has none, and SHALL alert once that shared prices aren't being read, and once when they are again.

#### Scenario: An old reader key
- **WHEN** a guard's reader key has no access to `guardrail.prices`
- **THEN** the guard prices with its release table and alerts once that the key needs the prices dataset in its reach

### Requirement: Operators can price models by hand when publishing
Publishing SHALL accept a file of manual model prices, per 1,000 input and output tokens. A manual price for a model the Price List doesn't price SHALL add that model to the published table. For a model the Price List prices, the published price SHALL be the higher of the two, input and output separately, and publishing SHALL say when the Price List is higher.

#### Scenario: Pricing a model the builder missed
- **WHEN** the Price List has no price for `xai.grok-4.6` and the file prices it at $0.003 in and $0.015 out
- **THEN** the published version prices `xai.grok-4.6` at $0.003 in and $0.015 out, marked as manual

#### Scenario: A manual price below the Price List
- **WHEN** the file prices `amazon.nova-pro-v1:0` at $0.0005 in, and the Price List at $0.0008
- **THEN** the published version keeps $0.0008, and publishing says the Price List price is higher

### Requirement: The manual price file is validated
Publishing SHALL refuse the file, and publish nothing, when it has an unknown field, a model with a missing, zero, or negative price, or the same model twice after normalizing inference profile prefixes.

#### Scenario: A zero price
- **WHEN** the file gives a model an output price of 0
- **THEN** publishing fails naming the model and the field, and no version is written

### Requirement: Manual prices carry forward until dropped
A published version SHALL record which of its prices are manual, and a later publish SHALL keep the latest version's manual prices unless the operator gives a new price for the model or drops it, or the Price List covers the model as the requirement on covered manual prices describes.

#### Scenario: A routine publish
- **WHEN** an operator publishes without a manual price file after a version that priced `xai.grok-4.6` by hand
- **THEN** the new version still prices `xai.grok-4.6` at its manual price

#### Scenario: Dropping a manual price
- **WHEN** an operator publishes with `--drop-override xai.grok-4.6` and a note
- **THEN** the new version doesn't price `xai.grok-4.6` unless the Price List does

#### Scenario: The Price List catches up
- **WHEN** `xai.grok-4.6` has a manual price of $0.003 in and $0.015 out, and the Price List starts pricing it at $0.003 in and $0.018 out
- **THEN** the next publish drops the manual price, publishes $0.003 in and $0.018 out, and its version note says the manual price was dropped because the Price List now covers it

### Requirement: Manual price changes need a reason
Publishing SHALL refuse to add, change, or drop a manual price without a note, so each manual price has a reason in the version history.

#### Scenario: No note
- **WHEN** an operator publishes a new manual price without `--note`
- **THEN** publishing fails asking for a note, and no version is written

### Requirement: Publishing warns when guards will price higher
When a manual price adds a model the release table doesn't list, and it is below the highest release price of that model's provider, publishing SHALL warn that guards will price the model at that higher floor.

#### Scenario: A cheap new model
- **WHEN** the file prices `xai.grok-4.6` at $0.001 in, and the release's highest xAI price is $0.0022 in
- **THEN** publishing warns that guards will price its input at $0.0022

### Requirement: A guard can publish prices on a schedule
When its stack enables price publishing, the guard SHALL build the AI price table from the Price List once a day and publish it by the same rules as the CLI, carrying manual prices forward. A day with no change SHALL write nothing. A stack without price publishing SHALL NOT build or publish prices.

#### Scenario: AWS adds a model
- **WHEN** the Price List prices a model the latest version doesn't list
- **THEN** the next daily run publishes a new version with that model, keeping every manual price

#### Scenario: Nothing changed
- **WHEN** the built table and the manual prices match the latest version
- **THEN** the daily run writes no version and sends no alert

### Requirement: Scheduled publishing reports failures once
When a scheduled publish fails, the guard SHALL send one alert per failing stretch saying what failed, and one alert when a publish succeeds again. A failed publish SHALL NOT write a partial version or stop the guard's other tasks.

#### Scenario: The publisher key expired
- **WHEN** two daily runs in a row fail because Jennah refuses the publisher key
- **THEN** one alert names the key and the error, and the next run that publishes sends one recovery alert

### Requirement: Scheduled publishing reports what changed
When a scheduled publish writes a version, the guard SHALL send one alert listing the models added, raised, lowered, and removed, and saying that guards apply only the added and raised ones.

#### Scenario: A price rise
- **WHEN** a daily run publishes a version that raises one model's output price
- **THEN** one alert names the model, its old and new prices, and the new version

### Requirement: Scheduled publishing reports newly unpriced models
When a build reports a model with no price that the latest version doesn't price by hand and that the previous build didn't already report, the guard SHALL send one alert naming the model and how to price it by hand.

#### Scenario: A model AWS hasn't priced
- **WHEN** Bedrock lists `xai.grok-5` and the Price List has no price for it
- **THEN** one alert names `xai.grok-5` and says to add a manual price, and later runs don't repeat it while it stays unpriced

### Requirement: Scheduled publishing reports manual prices above the Price List
When a scheduled publish finds a manual price that the Price List undercuts on input or output, the guard SHALL send one alert naming the model, its manual and Price List prices, and how to drop the manual price, and SHALL NOT repeat it while those prices stay the same.

#### Scenario: AWS prices a model below the operator's guess
- **WHEN** `xai.grok-4.6` has a manual price of $0.003 in and $0.015 out, and the Price List starts pricing it at $0.002 in and $0.010 out
- **THEN** the scheduled publish keeps the manual price, sends one alert naming both prices and the `--drop-override` command, and later runs don't repeat it

### Requirement: The builder prices every model the Price List prices per token
The price builder SHALL price a Bedrock text model whenever the Price List gives it regional or global on-demand input and output token rates, whether an entry names its direction in `inferenceType` (in any wording, such as "Text Input Token") or only in its usage type, and whether the model appears by name, by model id, or in a usage type with a suffix such as `-mantle`. A model with several rates for one side SHALL be priced at the highest.

#### Scenario: A model listed by id without an inference type
- **WHEN** the Price List lists `xai.grok-4.6` with usage types `USE1-xai.grok-4.6-mantle-input-tokens-standard` at $0.0022 and `...-output-tokens-standard` at $0.0066 per 1,000 tokens, and no `inferenceType`
- **THEN** the built table prices `xai.grok-4.6` at $0.0022 input and $0.0066 output

#### Scenario: Speech and text token rates
- **WHEN** the Price List gives Nova 2.5 Sonic speech rates of $0.003 input and $0.012 output and text rates of $0.00033 input and $0.00275 output
- **THEN** the built table prices `amazon.nova-2-5-sonic` at $0.003 input and $0.012 output

#### Scenario: A model AWS mislabels in one usage type
- **WHEN** Voxtral Small's standard usage types say `Voxtral-Mini-24B-2507` and its `-mantle` usage types carry `mistral.voxtral-small-24b-2507`
- **THEN** the built table prices `mistral.voxtral-small-24b-2507`, and Voxtral Mini's price is unchanged

#### Scenario: Not priced per token
- **WHEN** the Price List prices Rerank 3.5 per search unit and Pegasus per second of video
- **THEN** the builder reports them as having no price

### Requirement: A builder change never lowers a price
A change to how the builder matches Price List entries SHALL NOT lower the price it gives any model it priced before the change, from the same Price List entries.

#### Scenario: The recorded sample
- **WHEN** the builder runs on the recorded Price List sample before and after a change to its matching
- **THEN** every model priced before is priced the same or higher after

### Requirement: A manual price the Price List covers is dropped by itself
When the Price List prices a manual model at or above the manual price on both input and output, publishing SHALL drop the manual price without needing a note, and SHALL say so in the version's note.

#### Scenario: Equal prices
- **WHEN** a manual price equals the Price List's on both input and output
- **THEN** the next publish drops the manual price, publishes the same prices, and its note names the dropped model

### Requirement: A manual price above the Price List is kept
When the Price List prices a manual model below the manual price on input or output, publishing SHALL keep the higher price and suggest dropping the manual one.

#### Scenario: AWS prices a model lower
- **WHEN** a manual price is $0.015 output and the Price List's is $0.010
- **THEN** the published version keeps $0.015 output, and publishing suggests `--drop-override` for the model
