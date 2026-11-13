# account-coverage Specification

## Purpose
Makes sure every active account in a payer org is either protected by the guard's config or left out on purpose, and that someone is told about any account that is neither.

## Requirements
### Requirement: The guard checks coverage daily
At least daily, the guard SHALL list every active member account of its organization and determine, from the applied config, whether each is covered: named as an account scope, or a member of a scoped OU at any depth. Accounts that are suspended or closing SHALL be ignored.

#### Scenario: Covered through an OU
- **WHEN** account `222233334444` is in an OU that is a scope, and is not named itself
- **THEN** the daily check counts it as covered and reports nothing about it

#### Scenario: A suspended account
- **WHEN** an account in the organization is suspended
- **THEN** the check ignores it

### Requirement: Unprotected accounts are reported once
For each active account that is neither covered nor excluded, the guard SHALL write an `unprotected_account` event to the inbox and publish a local alert, the first time it finds the account so. It SHALL NOT report the same account again until the account has been covered or excluded and then becomes unprotected again.

#### Scenario: A new account
- **WHEN** account `555566667777` is created under the root and no scope or exclusion names it
- **THEN** the next daily check writes one `unprotected_account` event naming the account and its name, and publishes one local alert

#### Scenario: Reported once
- **WHEN** the same account is still unprotected on the following days
- **THEN** no further event or alert is written for it

#### Scenario: Covered, then uncovered again
- **WHEN** the account is added to a scope, and a later version removes that scope
- **THEN** after the removal is applied, the next check reports the account again

### Requirement: The config can exclude accounts
A config document MAY list `excluded_accounts`, account ids the guard SHALL never report as unprotected. An account SHALL NOT be both excluded and named as an account scope. The payer account SHALL always count as excluded. Changing `excluded_accounts` SHALL NOT need approval, since it changes no protection.

#### Scenario: An SSO account left out on purpose
- **WHEN** `excluded_accounts` contains the SSO account's id
- **THEN** the check never reports it

#### Scenario: Excluded and scoped at once
- **WHEN** a document lists an account in `excluded_accounts` and as an account scope
- **THEN** the document is invalid and the previous config stays applied

#### Scenario: Excluding does not need approval
- **WHEN** a version only adds an account to `excluded_accounts`
- **THEN** it applies on the next pull with no approval raised

### Requirement: Stale scopes are reported once
When an account scope names an account that is not an active member of the organization, the guard SHALL write a `stale_scope` event and publish a local alert, once per account until the scope is removed or the account returns.

#### Scenario: An account leaves the organization
- **WHEN** a scoped account is removed from the organization
- **THEN** the next daily check writes one `stale_scope` event naming it

### Requirement: Account events can trigger the check
When the stack's `ReactToAccountEvents` parameter is on, an account being created in, invited to, or moved within the organization SHALL trigger the same check within minutes. When it is off, the daily check SHALL be the only trigger.

#### Scenario: A new account with events on
- **WHEN** `ReactToAccountEvents` is on and a member account is created
- **THEN** its `unprotected_account` event is written within minutes, not at the next daily run

#### Scenario: Events off by default
- **WHEN** the stack is deployed without setting `ReactToAccountEvents`
- **THEN** no EventBridge rule for account events exists
