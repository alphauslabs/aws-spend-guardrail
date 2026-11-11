# Spec Delta

## ADDED Requirements

### Requirement: The install check uses AWS's SCP limit
`guardrail preflight` SHALL report a scope's target as having no free slot only when it has 10 SCPs attached directly, none of them a guard freeze policy, matching the AWS Organizations limit for a root, OU, or account. A target that already carries a freeze policy SHALL NOT be reported as full.

#### Scenario: A target with 5 policies
- **WHEN** a scope's account has 5 SCPs attached directly, none a freeze policy
- **THEN** preflight reports it as having free slots

#### Scenario: A target at the limit
- **WHEN** a scope's OU has 10 SCPs attached directly, none a freeze policy
- **THEN** preflight reports it as full and fails

#### Scenario: A target that already has the freeze policy
- **WHEN** a scope's account has 10 SCPs attached directly, one of them a guard freeze policy
- **THEN** preflight does not report it as full
