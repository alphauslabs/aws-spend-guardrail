## MODIFIED Requirements

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

## ADDED Requirements

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
