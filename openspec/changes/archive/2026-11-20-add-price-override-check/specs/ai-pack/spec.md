# Spec Delta

## ADDED Requirements

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
