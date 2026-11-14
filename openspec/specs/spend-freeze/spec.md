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

### Requirement: Local alerts reach the config's recipients
The guard SHALL subscribe each recipient in force that has no subscription on the local alert topic, and SHALL unsubscribe each confirmed subscription it made whose address is no longer a recipient. It SHALL record each address it subscribes or unsubscribes in the inbox. When an unsubscribe fails, it SHALL alert locally once for that address and keep trying on each run.

#### Scenario: A recipient is added
- **WHEN** an approved version adds `finops@example.com`
- **THEN** on that run the guard subscribes it, `finops@example.com` receives one confirmation email, and an event records the subscription

#### Scenario: A recipient is removed
- **WHEN** an approved version removes `finops@example.com`, whose subscription the guard made
- **THEN** the guard unsubscribes it, it receives no later alert, and an event records the removal

#### Scenario: A removal fails
- **WHEN** an approved version removes `finops@example.com`, and AWS refuses the guard's unsubscribe
- **THEN** one local alert says `finops@example.com` still receives alerts and why, the guard tries again on each run without alerting again, and the removal event is written once the unsubscribe succeeds

#### Scenario: Jennah is unreachable
- **WHEN** config refresh from Jennah fails
- **THEN** the recipients in force keep their subscriptions and receive the outage alert

### Requirement: Unconfirmed recipients aren't flooded
While a subscription the guard asked for awaits confirmation, the guard SHALL ask SNS again for that address at most once a day, and SHALL record the address as awaiting confirmation in the inbox once a day.

#### Scenario: Nobody confirms
- **WHEN** `finops@example.com` has not confirmed for two days
- **THEN** it has received at most two confirmation emails, and the inbox records it as awaiting confirmation

### Requirement: Config can't remove the stack's recipient or outside subscriptions
The guard SHALL NOT unsubscribe the address given in the stack's `AlertEmail`, or any subscription it did not make, whatever the config lists. The guard's role SHALL be allowed to subscribe, unsubscribe, and list subscriptions on the local alert topic only.

#### Scenario: Config lists nobody
- **WHEN** the version in force has no `alert_emails` and the stack's `AlertEmail` is `oncall@example.com`
- **THEN** `oncall@example.com` stays subscribed and receives every alert

#### Scenario: Config lists the stack's address, then drops it
- **WHEN** an approved version removes `oncall@example.com`, which is also the stack's `AlertEmail`
- **THEN** it stays subscribed

#### Scenario: A subscription added by hand
- **WHEN** someone subscribed `noc@example.com` to the alert topic in the console, and no version lists it
- **THEN** the guard leaves it subscribed

### Requirement: One stack installs the guard in a payer account
The guard SHALL be installed by one CloudFormation stack in the payer (management) account of one AWS organization, in us-east-1. The stack SHALL create the freeze policies, the role AWS Budgets assumes, the guard function, a local alert topic, and references to the guard's Jennah credentials. It SHALL NOT contain any budget.

#### Scenario: A fresh install changes nothing
- **WHEN** the stack is created in a payer account and no config version exists yet
- **THEN** the freeze policies exist but are attached to no target, no budget is created, and the guard reports that it has no config

#### Scenario: Budgets are not stack resources
- **WHEN** the guard creates, updates or deletes a budget
- **THEN** no stack update is needed, and the stack's resources are unchanged

### Requirement: Two freeze styles exist as unattached policies
The stack SHALL create two freeze policies, both unattached at install. Style A SHALL deny a fixed list of actions that create cost. Style B SHALL deny every action except, for a listed set of common services, reads and actions that reduce cost (stop, terminate, delete), and SHALL allow support, billing, cost and security tooling in full. Each scope SHALL select one style.

#### Scenario: Style B blocks a new launch
- **WHEN** an account frozen with style B calls `ec2:RunInstances`
- **THEN** the call is denied

#### Scenario: Style B allows cleanup
- **WHEN** an account frozen with style B calls `ec2:StopInstances` or `ec2:DescribeInstances`
- **THEN** the call is not denied by the freeze policy

#### Scenario: Style B blocks an unlisted service
- **WHEN** an account frozen with style B calls any action of a service that is not on the list, including a read
- **THEN** the call is denied, so a service the list misses is blocked rather than allowed

#### Scenario: Style A allows a deploy that adds no listed cost
- **WHEN** an account frozen with style A updates a Lambda function's code
- **THEN** the call is not denied by the freeze policy

### Requirement: The guard reconciles budgets from config
For every scope in the applied config, the guard SHALL ensure exactly one AWS cost budget it owns, limited to that scope, with the configured ceiling, period and threshold, and, for a scope in `enforce` mode, one action that attaches the scope's freeze policy at that threshold. A scope in `dry-run` mode SHALL have no budget action. Budgets the guard owns that no longer match any scope SHALL be deleted. Budgets it does not own SHALL never be changed.

#### Scenario: A new scope gets a budget
- **WHEN** an applied config adds a scope in `enforce` mode for account `111122223333` with a monthly ceiling of 50000 USD and a threshold of 90%
- **THEN** the guard creates one budget for that account with limit 50000 USD and one action that attaches the scope's freeze policy at 90% of actual cost

#### Scenario: A dry-run scope gets no action
- **WHEN** an applied config adds a scope in `dry-run` mode
- **THEN** the guard creates its budget and its notification at the threshold, and no budget action

#### Scenario: A customer's own budget is left alone
- **WHEN** the payer account has a budget the guard did not create
- **THEN** the guard neither updates nor deletes it

#### Scenario: Reconciling twice changes nothing
- **WHEN** the guard reconciles the same applied config twice
- **THEN** the second pass makes no change to any budget or action

### Requirement: An OU scope covers the OU's accounts
A scope MAY name an OU instead of an account. The budget for an OU scope SHALL cover the OU's member accounts at the time of reconciliation, and its freeze SHALL attach to the OU. The guard SHALL re-evaluate the OU's membership at least daily.

#### Scenario: An account joins a protected OU
- **WHEN** an account is moved into an OU that is a scope
- **THEN** by the next daily check the OU's budget includes that account's cost

### Requirement: The freeze needs neither Jennah nor the guard function
Once a budget and its action exist in enforce mode, AWS Budgets SHALL attach the freeze policy on its own when the threshold is reached. A freeze SHALL NOT depend on Jennah being reachable or on the guard function running.

#### Scenario: Jennah is down when the ceiling is reached
- **WHEN** a scope in enforce mode reaches its threshold while Jennah is unreachable
- **THEN** the freeze policy is attached to the scope's target, and the freeze event is recorded in Jennah once it is reachable again

### Requirement: Nothing running is stopped or deleted
The guard SHALL NOT stop, terminate, scale in, delete or modify any resource in a member account. Its only actions on member accounts and OUs SHALL be attaching and detaching the freeze policies, through AWS Budgets or directly.

#### Scenario: A frozen account keeps running
- **WHEN** an account with running instances is frozen
- **THEN** its running instances keep running and no resource in the account is changed by the guard

### Requirement: The payer account itself is never a scope
A config that names the payer account, or the organization root, as a scope SHALL be refused, because a freeze policy does not apply to the management account.

#### Scenario: The payer account is named
- **WHEN** a config version names the payer account's id as a scope
- **THEN** the version is refused as invalid and the previous config stays applied

### Requirement: Dry-run records a freeze without making it
A scope in `dry-run` mode SHALL have its budget, ceiling, threshold and notification reconciled like an enforcing scope, but no budget action, so no freeze policy can be attached and the budget is not billed as an action-enabled budget. When its threshold is reached, the guard SHALL record a `would_freeze` event.

#### Scenario: A dry-run scope crosses its threshold
- **WHEN** a dry-run scope's actual cost reaches its threshold
- **THEN** no freeze policy is attached and a `would_freeze` event naming the scope, ceiling and actual cost is recorded

#### Scenario: Switching to enforce adds the action
- **WHEN** an applied version moves a scope from `dry-run` to `enforce`
- **THEN** the next reconcile creates the scope's freeze action at its threshold

#### Scenario: Switching back to dry-run removes the action
- **WHEN** an applied version moves a scope from `enforce` to `dry-run`, after its loosening approval
- **THEN** the next reconcile deletes the scope's freeze action and keeps its budget and notification

#### Scenario: New scopes start in dry-run
- **WHEN** a config version adds a scope without stating its mode
- **THEN** the scope is in dry-run mode

### Requirement: Every freeze is recorded and alerted
When a freeze policy is attached to a scope's target, the guard SHALL record a `freeze` event and SHALL publish an alert to the stack's local alert topic. The local alert SHALL NOT depend on Jennah.

#### Scenario: A freeze is announced locally
- **WHEN** a scope in enforce mode is frozen
- **THEN** a message naming the scope, ceiling, threshold and actual cost is published to the local alert topic

### Requirement: Raising the budget lifts a freeze
When an applied config raises a frozen scope's ceiling so that actual cost is below the new threshold, the guard SHALL detach the scope's freeze policy and record a `lift` event. Raising a ceiling is a loosening and SHALL take effect only as `guard-config` allows.

#### Scenario: An approved raise lifts the freeze
- **WHEN** a frozen scope at 46000 USD actual cost has its ceiling raised from 50000 to 60000 USD with a 90% threshold, and the raise is applied
- **THEN** the freeze policy is detached from the scope's target and a `lift` event is recorded

#### Scenario: A raise that is still too low keeps the freeze
- **WHEN** the same scope's ceiling is raised to 51000 USD with a 90% threshold
- **THEN** the freeze stays attached, because actual cost is still above the new threshold

### Requirement: A new period lifts a freeze only when configured
Each scope SHALL have a month-end setting, `auto_lift` or `stay_frozen`, defaulting to `stay_frozen`. At the start of a new budget period, a frozen scope with `auto_lift` SHALL be lifted and a `lift` event recorded. A scope with `stay_frozen` SHALL remain frozen until its ceiling is raised.

#### Scenario: Auto lift at the new month
- **WHEN** a new month starts and a frozen scope has `auto_lift`
- **THEN** the freeze policy is detached and a `lift` event with reason `period_start` is recorded

#### Scenario: Stay frozen at the new month
- **WHEN** a new month starts and a frozen scope has `stay_frozen`
- **THEN** the freeze policy stays attached

### Requirement: Drift is detected and reported
At least daily, and after every reconcile, the guard SHALL compare the freeze policies' actual attachments and its owned budgets with what the applied config and recorded freezes imply. Any difference SHALL be recorded as a `drift` event and alerted locally. The guard SHALL NOT silently re-attach a freeze someone detached.

#### Scenario: A freeze is detached by hand
- **WHEN** an administrator detaches the freeze policy from a frozen account outside the guard
- **THEN** the next check records a `drift` event naming the account and alerts locally, and does not re-attach the policy
