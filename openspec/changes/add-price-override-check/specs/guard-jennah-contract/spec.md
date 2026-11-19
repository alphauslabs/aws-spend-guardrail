# Spec Delta

## ADDED Requirements

### Requirement: A low config price is an inbox event
When the guard reports that a payer org's `ai.prices` entry is below the price it otherwise knows, it SHALL write a `price_override_low` event whose detail gives the model, the entry's input and output prices, and the known input and output prices, per 1,000 tokens. Its id SHALL be the same for the same model, entry, and known price, so a retried write or a repeated run doesn't add a second event.

#### Scenario: A low entry
- **WHEN** the guard reports `openai.gpt-6.2-sol` at $0.0022 in and $0.011 out against a known $0.0025 in and $0.011 out
- **THEN** one `price_override_low` event carries the model, both prices, and the config version in force
