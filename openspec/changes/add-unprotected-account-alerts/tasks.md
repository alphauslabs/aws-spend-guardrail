# Tasks

## 1. AWS facts

- [x] 1.1 In the e2e organization, confirm whether Organizations account events reach EventBridge in us-east-1 without a CloudTrail trail; record the result in `docs/aws-facts.md` and adjust design D4 (confirmed: they don't, so `ReactToAccountEvents` needs a trail)

## 2. Config

- [x] 2.1 Add `excluded_accounts` to `config/schema.json`, the Go validator (refuse an account both excluded and scoped), and the schema agreement test; verify table tests for a valid exclusion, a duplicate with a scope, and an excluded account inside a scoped OU
- [x] 2.2 Classify `excluded_accounts` changes as `notice` in the diff, applied at once and left out of approval summaries (design D3); verify a test that a version changing only exclusions applies with no approval
- [x] 2.3 Document `excluded_accounts` in `docs/config.md` and add it to the examples; verify every schema field is still described

## 3. Guard

- [x] 3.1 Implement the coverage check in the daily run (design D1, D5): list active accounts, compute coverage from account and OU scopes, report unprotected and stale accounts; verify unit tests for a new account, coverage through an OU, a suspended account, an excluded account, and a scoped account that left the organization
- [x] 3.2 Report once with DynamoDB state items and deterministic event ids (design D2), and one combined alert per run; verify tests that a second day reports nothing, that cover-then-uncover reports again, and that the first run with many accounts sends one alert
- [x] 3.3 Handle the account events in the function's dispatch (design D4); verify a test that a `CreateAccount` event runs only the coverage check

## 4. Template and docs

- [x] 4.1 Add `organizations:ListAccounts` to the guard's role, and the `ReactToAccountEvents` parameter with a conditional EventBridge rule and invoke permission; verify `cfn-lint` passes and that the rule exists only when the parameter is on
- [ ] 4.2 Document the coverage check, `unprotected_account` and `stale_scope` events, and `ReactToAccountEvents` in `docs/install.md`, `docs/install-console.md` and `docs/operating.md`; verify the documented steps against the e2e stack

## 5. Integration

- [ ] 5.1 In the e2e organization, with the SSO account excluded, run the daily check and confirm the accounts left out of the test config are each reported once and the SSO account is not; then add one to the config and confirm the next run reports nothing for it

## Workflow follow-up

- Archive after `add-budget-freeze-guard` is archived, so its specs exist first.
- The first time a client with a CloudTrail trail turns on `ReactToAccountEvents`, confirm which events arrive for account creation, invitation and moves, and update `docs/aws-facts.md` and the rule's event names if they differ.
