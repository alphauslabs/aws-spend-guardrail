## ADDED Requirements

### Requirement: Caps per caller
The AI pack SHALL cut off a caller named in an AI scope when the caller's estimated AI spend in one account passes the hourly, daily, or monthly cap set for it. A caller in an OU scope SHALL have a separate budget in each member account. An assumed-role session SHALL count toward its role; an IAM user, including a Bedrock API key's user, SHALL count as itself.

#### Scenario: A role passes its daily cap
- **WHEN** role `app-chatbot` in account 111122223333 has a daily cap of $20 and its sessions' estimated spend today passes $20
- **THEN** a caller cutoff names the role, the account, the daily cap, and the total, and other callers in the account are not cut off

#### Scenario: One caller entry in an OU scope
- **WHEN** an OU scope caps role `app-chatbot` at $20 a day, and the role spends $15 today in each of two member accounts
- **THEN** neither account's role is cut off

#### Scenario: A long-term API key
- **WHEN** an OU scope caps user `BedrockAPIKey_ab12` at $100 a month and calls made with that key pass $100 this month in one account
- **THEN** that user is cut off in that account

### Requirement: A caller cutoff denies AI access to that caller only
In an `enforce` scope, a caller cutoff SHALL deny the caller every action the AI cutoff denies an account, on both the `bedrock-runtime` and `bedrock-mantle` endpoints, within 5 minutes, and SHALL deny nothing to other principals. In a `dry-run` scope, the guard SHALL record and alert what it would cut off, and deny nothing. Each caller cutoff SHALL be recorded as an inbox event naming the caller.

#### Scenario: Other roles keep working
- **WHEN** role `app-chatbot` in account 111122223333 is cut off in an enforce scope
- **THEN** its `Converse` calls and its `/v1/chat/completions` calls on the mantle endpoint are refused, and role `app-search` in the same account can still call `Converse`

#### Scenario: A role with a path
- **WHEN** a cut-off role is `AWSReservedSSO_Dev_0123abcd` under the path `/aws-reserved/sso.amazonaws.com/`
- **THEN** sessions of that role are refused

#### Scenario: Dry-run
- **WHEN** a caller passes its cap in a dry-run scope
- **THEN** a would-cut-off event names the caller, and no call is refused

### Requirement: A caller cutoff is lifted only by an approved loosening
A caller cutoff SHALL stay until a config version that lists its cutoff id under `ai.lifts`, raises the cap it passed, or removes that cap or caller entry, is approved and past the stack's delay. The lift SHALL be recorded. A caller cut off again after a lift SHALL get a new cutoff id.

#### Scenario: The cap is raised
- **WHEN** an approved version raises `app-chatbot`'s daily cap from $20 to $40, past the delay
- **THEN** the role's cutoff is lifted within 5 minutes and a lift event is recorded

### Requirement: A caller cutoff denies by the caller's unique id
A caller cutoff SHALL deny the caller by the unique IAM id its trail records carry, so it applies whatever the role's or user's path. When a call from a cut-off caller carries an id the cutoff doesn't hold yet, as after the role was deleted and created again with the same name, the guard SHALL add that id to the cutoff within 5 minutes, without a new cutoff id.

#### Scenario: A role created again
- **WHEN** cut-off role `app-chatbot` is deleted, created again with the same name, and calls Bedrock
- **THEN** within 5 minutes its calls are refused, and the cutoff keeps its id

### Requirement: The caller cutoff policies stay in step and within limits
The guard SHALL keep three caller cutoff policies, filled in order, each attached to the organization root only while it holds a cut-off caller in an enforce scope. When no policy has room for a caller within AWS's size limit, or the caller's unique id isn't known, the guard SHALL cut off that caller's whole account with the AI cutoff instead, and alert once. A change to a policy or its attachment made outside the guard SHALL be restored within a day, with one alert.

#### Scenario: The first policy is full
- **WHEN** a caller passes its cap and the first caller cutoff policy has no room for it
- **THEN** the caller is added to the second policy, which is attached to the root

#### Scenario: All policies are full
- **WHEN** a caller passes its cap and none of the three caller cutoff policies has room for it
- **THEN** the caller's account is cut off, the cutoff event names the caller and says the caller cutoff policies were full, and one alert is sent

#### Scenario: Nobody cut off any more
- **WHEN** the last caller cutoff is lifted
- **THEN** the guard detaches every caller cutoff policy from the root

#### Scenario: Edited by hand
- **WHEN** someone removes a role from a caller cutoff policy by hand
- **THEN** within a day the guard restores the policy and alerts once

### Requirement: Mantle usage doesn't count toward caller caps
Usage the guard reads from mantle metrics SHALL count toward the account's caps and SHALL NOT count toward any caller's caps, since the metrics carry no caller.

#### Scenario: Mantle-only spend
- **WHEN** role `app-chatbot` spends $30 today only through the mantle endpoint, and its daily cap is $20
- **THEN** the role is not cut off, and the $30 counts toward the account's daily cap
