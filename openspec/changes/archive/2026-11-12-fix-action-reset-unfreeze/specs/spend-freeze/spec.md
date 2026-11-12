## ADDED Requirements

### Requirement: A budget change keeps a freeze in force
When the guard updates the budget of a scope it has recorded as frozen, the scope SHALL still be frozen after the update, unless that run lifts it. If AWS Budgets resets the scope's executed action and detaches the freeze policy, the guard SHALL attach the policy again once the reset has finished, and SHALL record a `freeze_carried` event. It SHALL NOT write a second `freeze` event or send a second freeze alert for the same freeze.

#### Scenario: Tightening a scope frozen by the Budgets action
- **WHEN** a scope frozen by its Budgets action has its ceiling lowered, and AWS resets the action and detaches the policy a second after the guard's budget update
- **THEN** the freeze policy is attached again on that run, the scope's freeze record and its time are unchanged, and a `freeze_carried` event names the config version

#### Scenario: A raise that is still too low
- **WHEN** a scope frozen by its Budgets action has its ceiling raised but not above its spend
- **THEN** the scope is frozen after the run, as before the raise

#### Scenario: A raise that lifts the freeze
- **WHEN** a scope frozen by its Budgets action has its ceiling raised above its spend, and AWS's reset has already detached the policy
- **THEN** the guard records a `lift` and does not attach the policy again

#### Scenario: The reset takes longer than the run waits
- **WHEN** the action is still resetting after 30 seconds
- **THEN** the next run attaches the policy once the reset has finished
