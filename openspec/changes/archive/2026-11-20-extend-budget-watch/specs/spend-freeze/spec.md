## MODIFIED Requirements

### Requirement: A changed budget is watched until AWS Budgets reports spend again
After the guard creates or updates a budget, it SHALL read the scope's cost from Cost Explorer at least once an hour, starting on the same run, until the budget reports nonzero spend, the budget is gone, or a new budget period starts. Past 48 hours it SHALL stop only when Cost Explorer shows no spend for the scope this period. The AWS Budgets action SHALL stay in place and keep freezing on its own once it has spend.

#### Scenario: AWS Budgets catches up
- **WHEN** a changed budget reports nonzero spend again
- **THEN** the guard stops checking Cost Explorer for that scope, and the AWS Budgets action alone fires any later freeze

#### Scenario: A budget still at zero after 48 hours
- **WHEN** a budget changed 48 hours ago still reports zero, and Cost Explorer shows its scope spent 3.10 USD this month
- **THEN** the guard keeps reading Cost Explorer once an hour, and freezes the scope if that cost reaches its threshold

#### Scenario: A scope with no spend
- **WHEN** a budget changed 48 hours ago reports zero, and Cost Explorer also shows no spend for its scope this period
- **THEN** the guard stops watching it

## ADDED Requirements

### Requirement: A budget that hasn't caught up after 48 hours is reported
When a changed budget still reports zero after 48 hours while Cost Explorer shows its scope has spent, the guard SHALL send one alert naming the scope, how long it has waited, and the Cost Explorer cost. When that budget later reports spend, the guard SHALL send one more alert saying how long it took.

#### Scenario: A slow budget
- **WHEN** a budget changed at 09:00 on November 20 still reports zero at 09:00 on November 22, and Cost Explorer shows 3.10 USD
- **THEN** one alert says the budget hasn't caught up after 48 hours and the guard is still watching the scope's spend in Cost Explorer, and later runs send no more until the budget reports spend

#### Scenario: The budget catches up
- **WHEN** that budget reports spend at 15:00 on November 22
- **THEN** one alert says it caught up after 54 hours, and the watch ends
