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

### Requirement: Local alerts are written for a person
Every local alert body SHALL be written as one or more full sentences, starting with a capital letter and ending with a period. A target SHALL be named as "Account <id>" or "OU <id>", not in the `<kind>:<id>` form events use, and a reason SHALL be given in words, not as an internal code. Alert subjects SHALL stay short labels starting with `guardrail: `.

#### Scenario: A freeze alert
- **WHEN** account 891376926337 is frozen at 5.74 USD against a 5.00 USD ceiling with a 100% threshold
- **THEN** the body reads "Account 891376926337 is frozen: its actual cost of 5.74 USD reached 100% of its 5.00 USD ceiling (freeze style B). New spend is blocked, and nothing running was stopped."

#### Scenario: A lift alert
- **WHEN** a freeze is lifted because an approved raise put the ceiling above spend
- **THEN** the body names the account and says in words that its ceiling was raised above its spend, ending with a period, and contains no `budget_raised` code

### Requirement: Error detail in an alert is set apart
Error text from AWS, Jennah, or the guard SHALL NOT appear inside a sentence of an alert body. When an alert includes it, it SHALL follow the sentences on its own last line, starting with `Error: `.

#### Scenario: An alert carrying an error
- **WHEN** config can't be read because Jennah's name doesn't resolve
- **THEN** the body's first lines are sentences saying the last valid config stays in force and when the next reminder comes, and the last line is `Error: ` followed by the error text

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

### Requirement: Preflight counts the payer account's budget actions
`guardrail preflight` SHALL count the budget actions in the payer account on budgets the guard doesn't own, add the document's enforcing scopes, and fail when the total is over 100. Its output SHALL give both counts and the limit. Budget actions on budgets the guard owns SHALL NOT be counted twice: they are already among the document's enforcing scopes or will be removed.

#### Scenario: Other budget actions leave too little room
- **WHEN** the payer account has 30 budget actions on budgets the guard doesn't own and the document has 80 enforcing scopes
- **THEN** preflight fails, saying 30 actions are in use outside the guard, the document needs 80, and AWS allows 100

#### Scenario: Enough room
- **WHEN** the payer account has 5 budget actions outside the guard and the document has 60 enforcing scopes
- **THEN** preflight reports the counts and passes this check

### Requirement: A budget action AWS refuses for the limit is reported as a scope that can't freeze
When AWS refuses a scope's budget action because the payer account is at its limit of 100, the guard's alert SHALL name the scope, say it can't freeze until it has an action, give the limit, and name the remedy: fewer enforcing scopes (OU scopes or `dry-run`) or fewer budget actions outside the guard. The scope's budget SHALL still be written, and later runs SHALL try the action again.

#### Scenario: The 101st action is refused
- **WHEN** the payer account already has 100 budget actions and the effective config adds an enforcing scope for account `111122223333`
- **THEN** the guard's alert says account 111122223333 has no budget action and can't freeze because the payer account is at AWS's limit of 100 budget actions, and names the remedy

### Requirement: A refused budget action is alerted once
The alert for a scope whose action AWS refused for the limit SHALL be sent once while the refusal lasts, not on every run. When the scope gets its action, leaves the config, or leaves `enforce`, the condition SHALL clear, and a later refusal SHALL be alerted again.

#### Scenario: The alert isn't repeated every run
- **WHEN** the same scope's action is refused for the limit on three runs in a row
- **THEN** one alert is sent for it

#### Scenario: A pending loosening briefly needs more actions
- **WHEN** a version that replaces 10 enforcing scopes with 10 others waits for approval, so the stricter config in force holds both sets
- **THEN** the guard reports the scopes it can't create actions for, and once the version is approved and the removed scopes' actions go, it creates the rest on its next run
