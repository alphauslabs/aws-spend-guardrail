## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Manual prices carry forward until dropped
A published version SHALL record which of its prices are manual. A later publish SHALL keep the latest version's manual prices unless the operator gives a new price for the model or drops it, or the Price List prices the model at or above the manual price on both input and output. In that last case publishing SHALL drop the manual price without needing a note and SHALL say so in the version's note. When the Price List prices a manual model below the manual price on either side, publishing SHALL keep the higher price and suggest dropping the manual one.

#### Scenario: A routine publish
- **WHEN** an operator publishes without a manual price file after a version that priced `xai.grok-4.6` by hand
- **THEN** the new version still prices `xai.grok-4.6` at its manual price

#### Scenario: Dropping a manual price
- **WHEN** an operator publishes with `--drop-override xai.grok-4.6` and a note
- **THEN** the new version doesn't price `xai.grok-4.6` unless the Price List does

#### Scenario: The Price List catches up
- **WHEN** `xai.grok-4.6` has a manual price of $0.003 in and $0.015 out, and the Price List starts pricing it at $0.003 in and $0.018 out
- **THEN** the next publish drops the manual price, publishes $0.003 in and $0.018 out, and its version note says the manual price was dropped because the Price List now covers it
