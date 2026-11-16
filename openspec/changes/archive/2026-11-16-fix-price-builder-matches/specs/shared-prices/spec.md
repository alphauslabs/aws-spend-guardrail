## ADDED Requirements

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
