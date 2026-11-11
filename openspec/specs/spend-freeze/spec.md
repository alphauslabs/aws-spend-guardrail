# spend-freeze Specification

## Purpose
Freezes new AWS spend in an account or OU of a payer org when its budget ceiling is reached, using AWS Budgets and service control policies, without stopping or deleting anything already running and without depending on Jennah at the moment of the freeze.

## Requirements

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
