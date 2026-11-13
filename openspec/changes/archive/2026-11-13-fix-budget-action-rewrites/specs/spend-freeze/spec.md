# Spec Delta

## ADDED Requirements

### Requirement: Reconciling an unchanged scope writes nothing
When the guard reconciles a scope whose budget, notification, and (for an enforcing scope) freeze action already match config, it SHALL make no write to AWS Budgets: no budget update, no notification change, and no budget action update. A freeze action SHALL be updated only when its threshold, notification type, policy, target, execution role, approval model, or subscribers differ from what config wants, and a threshold type that Budgets leaves out of its response SHALL count as a percentage.

#### Scenario: Repeated runs on an enforcing scope
- **WHEN** the guard runs three times on an enforcing scope whose config hasn't changed
- **THEN** it updates neither the budget, its notification, nor its freeze action

#### Scenario: The threshold changes
- **WHEN** a new config version changes an enforcing scope's threshold from 90 to 80 percent
- **THEN** the next run updates the freeze action once, with the new threshold, and later runs write nothing

#### Scenario: The action was changed outside the guard
- **WHEN** someone changes a freeze action's execution role or subscribers in the console
- **THEN** the next run puts back what config wants
