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
Usage whose record can't carry a service tier, such as mantle usage read from metrics, SHALL be priced at the highest price of any tier its model has in that region, input and output separately. A model with no tier prices SHALL be priced at its standard price. `docs/aws-facts.md` SHALL list which records carry the tier.

#### Scenario: Mantle usage of a model with an Ultrafast tier
- **WHEN** an account's mantle usage is for a model whose standard price is $0.011 input and $0.055 output and whose Ultrafast price is $0.066 and $0.33 per 1,000 tokens
- **THEN** the usage is priced at $0.066 input and $0.33 output

#### Scenario: A model with only a standard price
- **WHEN** an account's mantle usage is for a model with no tier prices
- **THEN** the usage is priced at the model's standard price, as before

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
