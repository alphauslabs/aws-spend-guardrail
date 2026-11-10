# Spec Delta

## Purpose

Freezes new AWS spend in an account or OU of a payer org when its budget ceiling is reached, using AWS Budgets and service control policies, without stopping or deleting anything already running and without depending on Jennah at the moment of the freeze.

## ADDED Requirements

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
For every scope in the applied config, the guard SHALL ensure exactly one AWS cost budget it owns, limited to that scope, with the configured ceiling and period, and one action that attaches the scope's freeze policy at the configured threshold. Budgets the guard owns that no longer match any scope SHALL be deleted. Budgets it does not own SHALL never be changed.

#### Scenario: A new scope gets a budget
- **WHEN** an applied config adds a scope for account `111122223333` with a monthly ceiling of 50000 USD and a threshold of 90%
- **THEN** the guard creates one budget for that account with limit 50000 USD and one action that attaches the scope's freeze policy at 90% of actual cost

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
A scope in `dry-run` mode SHALL have its budget and threshold reconciled like an enforcing scope, but no freeze policy SHALL be attached. When its threshold is reached, the guard SHALL record a `would_freeze` event.

#### Scenario: A dry-run scope crosses its threshold
- **WHEN** a dry-run scope's actual cost reaches its threshold
- **THEN** no freeze policy is attached and a `would_freeze` event naming the scope, ceiling and actual cost is recorded

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
