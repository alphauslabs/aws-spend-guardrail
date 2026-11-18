## ADDED Requirements

### Requirement: Prices are published per region
A price version SHALL hold one AI chunk per region the builder priced, each in the current per-model format with that region's prices. The `us-east-1` chunk SHALL also list each model not offered in us-east-1 at its highest price in any region, so a guard of an earlier release, which reads only that chunk, prices it.

#### Scenario: A routine publish
- **WHEN** the builder prices models in 20 regions
- **THEN** the version lists 20 AI chunks, one per region, each with that region's prices

#### Scenario: A guard of an earlier release
- **WHEN** a v0.2.4 guard pulls a version with regional chunks
- **THEN** it reads only the `us-east-1` chunk, prices `deepseek.v3-v1:0` from it at its highest regional price, and works as before

### Requirement: The builder reads every region
The price builder SHALL read the Price List for every region and the Bedrock and `bedrock-mantle` model lists of every region the account running it has enabled. It SHALL price a mantle model id from usage types named by that id, a dated snapshot (`-YYYY-MM-DD`) at its base model's price, and a `moonshotai.` model at its `moonshot.` price. A region it can't list SHALL be named in its output, and the build SHALL go on.

#### Scenario: A model offered only outside us-east-1
- **WHEN** Bedrock offers `deepseek.v3-v1:0` in us-west-2 and ap-northeast-1 but not in us-east-1, and the Price List prices it in both
- **THEN** the built table prices it in those two regions

#### Scenario: A mantle-only model
- **WHEN** the mantle endpoint lists `google.gemma-4-31b`, and the Price List has `USE1-google.gemma-4-31b-mantle-input-tokens-standard` and `...-output-tokens-standard`
- **THEN** the built table prices `google.gemma-4-31b` in us-east-1

#### Scenario: A dated snapshot
- **WHEN** the mantle endpoint lists `openai.gpt-5.4-2026-03-05`, and the Price List prices OpenAI GPT-5.4
- **THEN** the built table prices the snapshot at GPT-5.4's prices

#### Scenario: A region the account hasn't enabled
- **WHEN** the publishing account hasn't enabled `ap-southeast-7`
- **THEN** the build prices the models listed elsewhere from the Price List's `ap-southeast-7` entries, and names `ap-southeast-7` as a region whose model list it couldn't read

### Requirement: Manual prices apply in every region
A manual price SHALL apply in every region: each region's price for the model SHALL be the higher of the manual price and that region's Price List price, input and output separately.

#### Scenario: A region the Price List prices higher
- **WHEN** `zai.glm-5.3` has a manual price of $0.001 input, and the Price List starts pricing it at $0.0012 in eu-west-2 only
- **THEN** eu-west-2 prices its input at $0.0012, and every other region at $0.001

## MODIFIED Requirements

### Requirement: The builder prices every model the Price List prices per token
The price builder SHALL price a Bedrock text model in each region whenever the Price List gives it regional or global on-demand input and output token rates there, whether an entry names its direction in `inferenceType` (in any wording, such as "Text Input Token") or only in its usage type, and whether the model appears by name, by model id, or in a usage type with a suffix such as `-mantle`. A model with several rates for one side in a region SHALL be priced at the highest.

#### Scenario: A model listed by id without an inference type
- **WHEN** the Price List lists `xai.grok-4.6` with usage types `USE1-xai.grok-4.6-mantle-input-tokens-standard` at $0.0022 and `...-output-tokens-standard` at $0.0066 per 1,000 tokens, and no `inferenceType`
- **THEN** the built table prices `xai.grok-4.6` at $0.0022 input and $0.0066 output in us-east-1

#### Scenario: Speech and text token rates
- **WHEN** the Price List gives Nova 2.5 Sonic speech rates of $0.003 input and $0.012 output and text rates of $0.00033 input and $0.00275 output
- **THEN** the built table prices `amazon.nova-2-5-sonic` at $0.003 input and $0.012 output

#### Scenario: A model AWS mislabels in one usage type
- **WHEN** Voxtral Small's standard usage types say `Voxtral-Mini-24B-2507` and its `-mantle` usage types carry `mistral.voxtral-small-24b-2507`
- **THEN** the built table prices `mistral.voxtral-small-24b-2507`, and Voxtral Mini's price is unchanged

#### Scenario: Not priced per token
- **WHEN** the Price List prices Rerank 3.5 per search unit and Pegasus per second of video
- **THEN** the builder reports them as having no price

#### Scenario: Prices differ by region
- **WHEN** the Price List prices a model's input at $0.004 in us-east-1 and $0.0062 in eu-west-2
- **THEN** the built table prices it at $0.004 in us-east-1 and $0.0062 in eu-west-2

### Requirement: A manual price the Price List covers is dropped by itself
When the Price List prices a manual model at or above the manual price on both input and output in every region the builder found it offered in, publishing SHALL drop the manual price without needing a note, and SHALL say so in the version's note.

#### Scenario: Equal prices
- **WHEN** a manual price equals the Price List's on both input and output in every region the model is offered in
- **THEN** the next publish drops the manual price, publishes the same prices, and its note names the dropped model

#### Scenario: Priced in some regions only
- **WHEN** `zai.glm-5.3` has a manual price, and the Price List starts pricing it at or above it in us-east-1 but not in eu-central-1, where Bedrock also offers it
- **THEN** the manual price is kept, and eu-central-1 uses it
