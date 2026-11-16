## ADDED Requirements

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
A published version SHALL record which of its prices are manual. A later publish SHALL keep the latest version's manual prices unless the operator gives a new price for the model or drops it. When the Price List starts pricing a manual model, publishing SHALL keep the higher price and suggest dropping the manual one.

#### Scenario: A routine publish
- **WHEN** an operator publishes without a manual price file after a version that priced `xai.grok-4.6` by hand
- **THEN** the new version still prices `xai.grok-4.6` at its manual price

#### Scenario: Dropping a manual price
- **WHEN** an operator publishes with `--drop-override xai.grok-4.6` and a note
- **THEN** the new version doesn't price `xai.grok-4.6` unless the Price List does

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
