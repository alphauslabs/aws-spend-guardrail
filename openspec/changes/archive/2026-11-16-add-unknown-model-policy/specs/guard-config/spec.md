## MODIFIED Requirements

### Requirement: The document configures the AI pack
The config document SHALL accept an optional `ai` section with: AI scopes per account or OU, each with hourly, daily, and monthly caps in USD and a mode; defaults for those; model and region allowlists; the callers allowed to use Bedrock API keys; per-model price overrides; `lifts`, a list of cutoff ids to lift; and `unknown_models`, either `cutoff` or `count`, saying how a call to a model with no price is treated. `unknown_models` SHALL default to `cutoff`.

#### Scenario: A minimal AI section
- **WHEN** a document sets `ai.defaults.monthly_usd` to 500 and lists one account under `ai.scopes`
- **THEN** it validates, and that account has a $500 monthly cap in dry-run mode

#### Scenario: Unknown models by default
- **WHEN** a document's `ai` section doesn't set `unknown_models`
- **THEN** a call to a model with no price trips the `unknown_model` tripwire

### Requirement: The AI section is validated
Validation SHALL refuse an unknown field in the `ai` section, a negative cap, a scope that names an account or OU twice, a price override with a missing or negative price, a lift naming no cutoff id, and an `unknown_models` value other than `cutoff` or `count`.

#### Scenario: An invalid AI section
- **WHEN** a document gives a scope a negative hourly cap
- **THEN** validation fails naming the field

#### Scenario: An invalid unknown-models value
- **WHEN** a document sets `ai.unknown_models` to `allow`
- **THEN** validation fails naming the field and the two values it accepts

### Requirement: AI changes are classified like other changes
A lower cap, a model or region removed from an allowlist, a caller removed from the API-key list, a higher price override, a move from dry-run to enforce, and a move of `unknown_models` from `count` to `cutoff` SHALL be tightening and apply at once. A higher or removed cap, an added model, region, or API-key caller, a lower or removed price override, a move from enforce to dry-run, a new lift, and a move of `unknown_models` from `cutoff` to `count` SHALL be loosening and need the approval and delay.

#### Scenario: Adding a model
- **WHEN** a version adds a model to the allowlist
- **THEN** it waits for approval, and the approval lists the added model

#### Scenario: Lifting a cutoff
- **WHEN** a version lists cutoff `ai:111122223333:20261111T0930` under `ai.lifts`
- **THEN** one approval is raised for it, showing the cutoff and the version's reason

#### Scenario: Counting unpriced models
- **WHEN** a version sets `ai.unknown_models` to `count` where the version in force has `cutoff`
- **THEN** it waits for approval, and the approval says calls to models with no price will be counted at the highest known price instead of cutting off the account
