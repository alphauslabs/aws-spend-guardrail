## ADDED Requirements

### Requirement: The AI section sets how unmeasured calls are treated
The `ai` section SHALL accept `unmeasured_calls`, either `cutoff` or `allow`, saying how a call the guard can't measure is treated, defaulting to `cutoff`. Validation SHALL refuse any other value. A move from `cutoff` to `allow`, including `allow` in a first version, SHALL be loosening; a move from `allow` to `cutoff` SHALL be tightening.

#### Scenario: Not set
- **WHEN** a document's `ai` section doesn't set `unmeasured_calls`
- **THEN** unmeasured calls cut the account off

#### Scenario: A bad value
- **WHEN** a document sets `ai.unmeasured_calls` to `count`
- **THEN** validation refuses the document and names the field

#### Scenario: Allowing needs approval
- **WHEN** a version sets `ai.unmeasured_calls` to `allow` where the version in force has `cutoff`
- **THEN** the change is loosening and applies only after approval and the stack's delay
