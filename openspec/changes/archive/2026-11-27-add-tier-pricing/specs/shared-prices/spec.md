## MODIFIED Requirements

### Requirement: The builder prices every model the Price List prices per token
The price builder SHALL price a Bedrock text model in each region where the Price List gives it regional or global on-demand input and output token rates, whether an entry names its direction in `inferenceType` (in any wording) or only in its usage type, and whether it names the model by name, by id, or in a suffixed usage type such as `-mantle`. Its standard price SHALL be the highest of its standard rates per side; its Flex, Priority, and Ultrafast rates SHALL be its tier prices.

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

#### Scenario: A model with premium tiers
- **WHEN** the Price List gives GPT-6 Astra standard rates of $11 and $55 and Ultrafast rates of $66 and $330 per 1M tokens in us-east-1
- **THEN** the built table prices it at $0.011 input and $0.055 output, with an Ultrafast tier price of $0.066 and $0.33

## ADDED Requirements

### Requirement: Tier prices are published and merged tighten-only
Tier prices SHALL be published in each region's chunk with the model they belong to, and compared, reported, and carried forward like standard prices, with each change naming its tier. A guard SHALL merge each tier's release and shared prices tighten-only, as for standard prices. Guards of releases without tier pricing SHALL keep reading a chunk with tier prices, at its standard prices.

#### Scenario: A new tier is published
- **WHEN** the Price List adds a Priority rate for a model and the scheduled publish runs
- **THEN** a new version is written, and its alert lists the model's Priority price as added

#### Scenario: An older guard reads the chunk
- **WHEN** a guard of v0.2.17 reads a chunk with tier prices
- **THEN** it reads the chunk without error and prices calls at the standard prices
