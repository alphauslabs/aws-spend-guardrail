# Spec Delta

## ADDED Requirements

### Requirement: A low config price is an inbox event
When the guard reports that a payer org's `ai.prices` entry is below the price it otherwise knows, it SHALL write a `price_override_low` event whose detail gives the model, the entry's input and output prices, and the known input and output prices, per 1,000 tokens. Its id SHALL be the same for the same model, entry, known price, and UTC day, so a retried write doesn't add a second event, while a report made again on a later day is its own event.

#### Scenario: A low entry
- **WHEN** the guard reports `openai.gpt-6.2-sol` at $0.0022 in and $0.011 out against a known $0.0025 in and $0.011 out
- **THEN** one `price_override_low` event carries the model, both prices, and the config version in force
