# Design

## Context

See proposal.md for why. The guard (`add-budget-freeze-guard`) is one Lambda in the payer account with several triggers, including a daily run that already lifts freezes at period start, checks drift, refreshes OU membership and writes a heartbeat. It already reads OU membership with `ListAccountsForParent` and `ListOrganizationalUnitsForParent`, keeps state in DynamoDB, and sends events through an SQS outbox to the Jennah inbox.

## Goals / Non-Goals

**Goals:**
- Every active account is either protected, excluded on purpose, or reported.
- No new function, and nothing new on the freeze path.

**Non-Goals:**
- Choosing a ceiling for a new account.
- Real-time detection without CloudTrail.

## Decisions

### D1. One more step in the daily run
`Daily` calls a coverage check after drift. It lists all accounts with `organizations:ListAccounts` (paginated), keeps those with state `ACTIVE`, and builds the covered set: account scopes, plus the members of each OU scope, which reconcile already computes. Covered, excluded and the payer account are removed; what's left is unprotected.

*Alternative:* a separate scheduled function. Rejected: it duplicates setup, credentials and state access for a few API calls a day.

### D2. Report once, with one state item per account
DynamoDB item `pk=reported, sk=<kind>#<account id>` records that an account was reported. A report is written only when the item is absent. When the account becomes covered or excluded, the check deletes the item, so a later lapse is reported again. Event ids are `unprotected_account:<account id>:<first seen date>` and `stale_scope:<account id>:<first seen date>`, so a retried delivery never duplicates.

### D3. `excluded_accounts` is neither tightening nor loosening
It changes what's reported, not what's protected, so a change to it applies at once with no approval, like a tightening. The diff classifies it separately as `notice` and the approval summary never mentions it. Validation refuses an account that is both excluded and an account scope, and accepts an excluded account inside a scoped OU (the OU still covers it; the exclusion just has no effect).

### D4. Account events through EventBridge, optional
With `ReactToAccountEvents=true`, the template adds a rule matching `source: aws.organizations`, `detail-type: AWS API Call via CloudTrail`, and event names `CreateAccount`, `InviteAccountToOrganization`, `AcceptHandshake` and `MoveAccount`, targeting the guard. (`CreateAccount` returns before the account exists; task 1.1 confirms which event marks it active.) The function recognizes the event shape and runs the coverage check only. Account creation is asynchronous, so the check also runs on the next daily run in case the account wasn't active yet.

*Why optional:* EventBridge gets these events only through CloudTrail. Confirmed in the e2e organization, which has no trail: two `MoveAccount` calls appeared in CloudTrail event history, but an EventBridge rule on `aws.organizations` matched nothing. So the parameter needs a trail recording management events, and its description and the install docs say so. Which events arrive with a trail is still to confirm.

### D5. Messages name the account
Each event and alert carries the account id, its name from Organizations, and its parent (root or OU), so the person reading it can decide the ceiling without looking it up.

## Risks / Trade-offs

- [`ListAccounts` returns thousands of accounts for a large distributor] → It is paginated and runs once a day; one call per 20 accounts is a few seconds at most.
- [An account created and immediately placed in a scoped OU is briefly reported as unprotected] → With events on, creation may be seen before the move. The check runs on the move event too, and the report is skipped if the account is covered by then; a report already sent is followed by no further noise because coverage deletes its state item silently.
- [Alert fatigue at first install, when many accounts are uncovered] → The first run sends one alert listing all unprotected accounts, not one per account. Events stay one per account.
