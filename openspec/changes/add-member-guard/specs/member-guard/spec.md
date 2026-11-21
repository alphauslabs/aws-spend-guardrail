# Spec Delta

## Purpose

Lets a client cap AI spend per caller inside one of its own member accounts: the member guard counts each Bedrock call toward the role, session, IAM user, or API key that made it, and denies only that caller when it passes its cap.

## ADDED Requirements

### Requirement: The member guard reads its own account's Bedrock calls
The member guard SHALL read the Bedrock runtime calls its account makes, in every region it is installed for, from CloudTrail events on that account's EventBridge buses, with nothing installed in other accounts. It SHALL price each call within 2 minutes of the call.

#### Scenario: A call in another region
- **WHEN** the member guard's home region is us-east-1, it is installed for us-west-2, and a role calls `Converse` in us-west-2
- **THEN** the call is priced and counted within 2 minutes

#### Scenario: A region not installed
- **WHEN** a role calls `Converse` in a region the member guard isn't installed for
- **THEN** the call is not counted, and the daily heartbeat lists the region as seen in the account's calls but not covered

### Requirement: The member guard reports when it receives no events
The member guard SHALL alert once when the account has no CloudTrail trail recording management events, so its rules receive nothing, and once again when events arrive.

#### Scenario: No trail
- **WHEN** the member guard is installed in an account with no trail recording management events, in an organization with no organization trail
- **THEN** it alerts once that it can't see any calls and says a trail is needed

### Requirement: Calls are priced as the guard prices them
The member guard SHALL price each call with the same release price table, shared prices, regional prices, and unpriced-model rule as the guard of the same source commit, and SHALL count a call that CloudTrail delivers twice only once.

#### Scenario: A model only shared prices list
- **WHEN** a caller uses a model that only the shared prices list
- **THEN** the member guard prices it as the guard would

#### Scenario: A duplicate event
- **WHEN** EventBridge delivers the same CloudTrail event twice
- **THEN** its cost is counted once

### Requirement: Each call is attributed to one caller
The member guard SHALL attribute each call to its caller: an assumed-role session to its role, and to its session name as well; an IAM user, including the user behind a long-term Bedrock API key, to that user; a short-term API key to the role session that created it. Calls by the root user or an AWS service SHALL be attributed to no caller.

#### Scenario: A long-term API key
- **WHEN** a call is made with a long-term Bedrock API key whose IAM user is `BedrockAPIKey_ab12`
- **THEN** it counts toward user `BedrockAPIKey_ab12`

#### Scenario: A role with a path
- **WHEN** a session of role `ci-runner` under path `/build/` makes a call
- **THEN** it counts toward role `ci-runner`

### Requirement: Totals are kept for named callers only
The member guard SHALL keep each named caller's estimated spend over the last hour, the current UTC day, and the current calendar month, counting a call at the time CloudTrail recorded it. Totals for a caller SHALL start when its entry first applies. Calls from callers no entry names SHALL NOT be stored.

#### Scenario: A caller added mid-day
- **WHEN** an entry for role `app-chatbot` applies at 14:00 and the role spent $10 that morning
- **THEN** the role's daily total includes none of the morning's $10

### Requirement: A caller is cut off when it passes a cap
When a named caller's total passes one of its caps, the member guard SHALL cut off that caller within 2 minutes of the call that passed it, and SHALL deny nothing to other callers. The cutoff SHALL be recorded as an inbox event naming the caller, the window, the cap, and the total.

#### Scenario: A role passes its daily cap
- **WHEN** role `app-chatbot` has a daily cap of $20 and its spend today passes $20
- **THEN** its `Converse` calls are refused within 2 minutes, and role `app-search` in the same account still works

#### Scenario: A user passes its monthly cap
- **WHEN** user `BedrockAPIKey_ab12` has a monthly cap of $100 and calls with its key pass $100 this month
- **THEN** calls made with that key are refused

### Requirement: Sessions of one role can have their own budgets
For a role entry with `per_session` set, the member guard SHALL keep totals for each session name of that role separately, and SHALL cut off only the session name that passes a cap, including its later sessions with the same name.

#### Scenario: One developer passes the cap
- **WHEN** role `claude-code` has `per_session` and a daily cap of $20, and session `alice@example.com` spends $21 today while session `bob@example.com` spends $5
- **THEN** sessions named `alice@example.com` are refused, including one started after the cutoff, and sessions named `bob@example.com` still work

### Requirement: A cutoff denies AI actions only
A cutoff SHALL deny the caller the same AI actions the guard's AI cutoff denies, on both the `bedrock-runtime` and `bedrock-mantle` endpoints, and nothing else.

#### Scenario: A cut-off role keeps other access
- **WHEN** role `app-chatbot` is cut off
- **THEN** its `Converse` and mantle chat completions calls are refused, and its S3 and DynamoDB calls are unaffected

### Requirement: Dry-run records without denying
In dry-run mode, the member guard SHALL record and alert a would-cutoff event when a caller passes a cap, at most once per caller and window, and SHALL deny nothing.

#### Scenario: Dry-run
- **WHEN** a caller passes its daily cap in dry-run mode
- **THEN** one would-cutoff event names the caller, and its calls still succeed

### Requirement: SSO callers are counted but not denied
The member guard SHALL count and cap callers whose role was created by IAM Identity Center, and SHALL record a would-cutoff event instead of a cutoff when one passes a cap, saying the role can't be changed from a member account.

#### Scenario: An SSO permission set passes its cap
- **WHEN** a caller entry names role `AWSReservedSSO_Dev_0123abcd` and its spend passes the cap in enforce mode
- **THEN** a would-cutoff event names the role and says SSO roles can't be denied from the account, and its calls still succeed

### Requirement: Mantle usage doesn't count toward caller caps
The member guard SHALL NOT count `bedrock-mantle` usage toward any caller's totals, since mantle reports usage without a caller.

#### Scenario: Mantle-only spend
- **WHEN** role `app-chatbot` spends $30 today only through the mantle endpoint and its daily cap is $20
- **THEN** the role is not cut off

### Requirement: A cutoff lifts when its window resets
A cutoff SHALL lift when the window of the cap it passed resets (the next hour for an hourly cap, 00:00 UTC for a daily cap, the first of the month for a monthly cap), unless the caller is over another cap at that time. Each lift SHALL be recorded as an inbox event.

#### Scenario: A new day
- **WHEN** role `app-chatbot` was cut off for its daily cap at 15:00 UTC and has no other cap passed
- **THEN** at 00:00 UTC the next day its calls succeed again, and a lift event is recorded

#### Scenario: Still over the monthly cap
- **WHEN** a role cut off for its daily cap is also over its monthly cap at 00:00 UTC
- **THEN** it stays cut off, now for its monthly cap

### Requirement: A cutoff lifts early only by an approved loosening
Before its window resets, a cutoff SHALL lift only when an approved config version raises or removes the cap it passed, removes the caller entry, or lists its cutoff id under `lifts`.

#### Scenario: The cap is raised
- **WHEN** an approved version raises `app-chatbot`'s daily cap from $20 to $40 while today's total is $25
- **THEN** the role's cutoff is lifted within 2 minutes of the version applying

### Requirement: Cutoffs changed outside the member guard are restored
When a cutoff's deny policy is detached, edited, or deleted by someone else, the member guard SHALL restore it within 15 minutes and alert once. It SHALL NOT be able to stop the account's admins from removing the member guard itself.

#### Scenario: An admin detaches the deny policy
- **WHEN** an admin detaches the deny policy from a cut-off role
- **THEN** within 15 minutes the policy is attached again, one alert names the role, and a restored-cutoff event is recorded

### Requirement: The member guard never cuts off itself
The member guard SHALL refuse a config that names its own role, and SHALL NOT attach a deny policy to its own role or to roles its stack created.

#### Scenario: Naming the guard's role
- **WHEN** a config names the member guard's function role as a caller
- **THEN** the version is rejected naming the entry

### Requirement: Removing the member guard lifts its cutoffs
Deleting the member guard's stack SHALL detach and delete every deny policy the member guard created, so no caller stays cut off.

#### Scenario: The stack is deleted with a cutoff in place
- **WHEN** a client deletes the member stack while role `app-chatbot` is cut off
- **THEN** after the deletion completes, the role's calls succeed and no policy under the member guard's path remains
