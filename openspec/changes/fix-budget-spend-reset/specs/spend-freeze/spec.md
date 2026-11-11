## ADDED Requirements

### Requirement: Lift decisions use Cost Explorer spend
When the guard decides whether a raised ceiling lifts a frozen scope, it SHALL compare the new threshold with the scope's actual cost from Cost Explorer for the scope's accounts over the current budget period to date, not with the budget's calculated spend. If Cost Explorer can't be read, the guard SHALL NOT lift the freeze, SHALL alert, and SHALL try again on a later run. The `lift` event SHALL record the cost it used and `spend_source` `cost_explorer`.

#### Scenario: A raise that is still too low keeps the freeze on later runs
- **WHEN** a scope frozen at a 4 USD ceiling with 5.74 USD spent has its ceiling raised to 5 USD, and the budget update makes the budget report 0 USD
- **THEN** the freeze stays attached on that run and every later run while Cost Explorer reports at least 5 USD for the period

#### Scenario: A raise above spend lifts the freeze
- **WHEN** the same scope's ceiling is raised to 8 USD and Cost Explorer reports 5.74 USD
- **THEN** the freeze policy is detached and a `lift` event records 5.74 USD and `spend_source` `cost_explorer`

#### Scenario: Cost Explorer is unavailable
- **WHEN** a frozen scope's ceiling is raised and the Cost Explorer read fails
- **THEN** the freeze stays attached and an alert says the lift is waiting on Cost Explorer

### Requirement: A changed budget is watched until AWS Budgets reports spend again
After the guard creates or updates a budget, the guard SHALL read the scope's actual cost from Cost Explorer at least once an hour, starting on the same run, until the budget reports nonzero spend again, 48 hours pass, or a new budget period starts. The AWS Budgets action SHALL stay in place and keep freezing on its own once it has spend.

#### Scenario: AWS Budgets catches up
- **WHEN** a changed budget reports nonzero spend again
- **THEN** the guard stops checking Cost Explorer for that scope, and the AWS Budgets action alone fires any later freeze

### Requirement: The guard freezes a watched scope that is over its threshold
When a watched scope's Cost Explorer cost is at or over its threshold, a scope in `enforce` SHALL be frozen by the guard attaching its freeze policy, recorded with a `freeze` event (`how` `guard_attach`, `spend_source` `cost_explorer`), and alerted like any other freeze. A scope in `dry-run` SHALL record `would_freeze` instead. A scope already frozen SHALL NOT be frozen again.

#### Scenario: A lower ceiling below current spend freezes within the hour
- **WHEN** a version lowers an enforcing scope's ceiling from 100 to 4 USD while Cost Explorer reports 5.24 USD, and the budget reports 0 after the update
- **THEN** within about an hour of the update the freeze policy is attached, a `freeze` event records `guard_attach` and 5.24 USD, and the local alert is sent

#### Scenario: Spend crosses the threshold while the budget still reads zero
- **WHEN** a changed budget still reports 0, and Cost Explorer shows the scope's cost crossing its threshold 10 hours after the change
- **THEN** the guard freezes the scope on its next hourly check

#### Scenario: A dry-run scope
- **WHEN** a dry-run scope's budget was changed and Cost Explorer shows its cost over the threshold
- **THEN** the guard records `would_freeze` and attaches nothing

### Requirement: Freeze and lift events say where their figures came from
Every `freeze`, `would_freeze`, and `lift` event SHALL carry `spend_source` (`budget` or `cost_explorer`), and every `freeze` event SHALL carry `how` (`budget_action` or `guard_attach`).

#### Scenario: A freeze made by AWS Budgets
- **WHEN** AWS Budgets attaches the freeze policy and notifies the guard
- **THEN** the `freeze` event records `how` `budget_action` and `spend_source` `budget`
