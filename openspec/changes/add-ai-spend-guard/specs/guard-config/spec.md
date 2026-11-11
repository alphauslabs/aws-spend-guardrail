# Spec Delta

## ADDED Requirements

### Requirement: The document configures the AI pack
The config document SHALL accept an optional `ai` section with: AI scopes per account or OU, each with hourly, daily, and monthly caps in USD and a mode; defaults for those; model and region allowlists; the callers allowed to use Bedrock API keys; per-model price overrides; and `lifts`, a list of cutoff ids to lift.

#### Scenario: A minimal AI section
- **WHEN** a document sets `ai.defaults.monthly_usd` to 500 and lists one account under `ai.scopes`
- **THEN** it validates, and that account has a $500 monthly cap in dry-run mode

### Requirement: The AI section is validated
Validation SHALL refuse an unknown field in the `ai` section, a negative cap, a scope that names an account or OU twice, a price override with a missing or negative price, and a lift naming no cutoff id.

#### Scenario: An invalid AI section
- **WHEN** a document gives a scope a negative hourly cap
- **THEN** validation fails naming the field

### Requirement: AI changes are classified like other changes
A lower cap, a model or region removed from an allowlist, a caller removed from the API-key list, a higher price override, and a move from dry-run to enforce SHALL be tightening and apply at once. A higher or removed cap, an added model, region, or API-key caller, a lower or removed price override, a move from enforce to dry-run, and a new lift SHALL be loosening and need the approval and delay.

#### Scenario: Adding a model
- **WHEN** a version adds a model to the allowlist
- **THEN** it waits for approval, and the approval lists the added model

#### Scenario: Lifting a cutoff
- **WHEN** a version lists cutoff `ai:111122223333:20261111T0930` under `ai.lifts`
- **THEN** one approval is raised for it, showing the cutoff and the version's reason
