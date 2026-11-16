## MODIFIED Requirements

### Requirement: AI changes are classified like other changes
Each change to the `ai` section SHALL be classified as tightening, which applies at once, or loosening, which needs the approval and delay, as the requirements on AI tightening and AI loosening changes list.

#### Scenario: Adding a model
- **WHEN** a version adds a model to the allowlist
- **THEN** it waits for approval, and the approval lists the added model

#### Scenario: Lifting a cutoff
- **WHEN** a version lists cutoff `ai:111122223333:20261111T0930` under `ai.lifts`
- **THEN** one approval is raised for it, showing the cutoff and the version's reason

#### Scenario: Counting unpriced models
- **WHEN** a version sets `ai.unknown_models` to `count` where the version in force has `cutoff`
- **THEN** it waits for approval, and the approval says calls to models with no price will be counted at the highest known price instead of cutting off the account

## ADDED Requirements

### Requirement: AI tightening changes
A lower cap, a model or region removed from an allowlist, a caller removed from the API-key list, a higher price override, a move from dry-run to enforce, and a move of `unknown_models` from `count` to `cutoff` SHALL be tightening and apply at once.

#### Scenario: A lower cap
- **WHEN** a version lowers an account's AI hourly cap from $20 to $10
- **THEN** the guard applies it on its next pull, with no approval

### Requirement: AI loosening changes
A higher or removed cap, an added model, region, or API-key caller, a lower or removed price override, a move from enforce to dry-run, a new lift, and a move of `unknown_models` from `cutoff` to `count` SHALL be loosening and need the approval and delay.

#### Scenario: A higher cap
- **WHEN** a version raises an account's AI hourly cap from $10 to $20
- **THEN** it waits for approval and the delay before it applies
