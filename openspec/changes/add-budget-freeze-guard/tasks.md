# Tasks

## 1. Repo setup and AWS facts

- [ ] 1.1 In a test AWS organization, confirm each fact the design relies on, and record results in `docs/aws-facts.md`:
  - a Budgets action of type `APPLY_SCP_POLICY` can target a member account and an OU;
  - an action added to a budget that is already above its threshold runs (a dry-run scope over budget that moves to enforce must still freeze);
  - `ExecuteBudgetAction` with `REVERSE_BUDGET_ACTION` detaches the policy;
  - what happens to an executed action at the start of a new period;
  - how many actions one budget may have, and the budgets-per-account limit;
  - the Budgets refresh cadence;
  - the SCP size limit and the per-target limit;
  - that an SCP accepts service-wildcard patterns like `*:Describe*` in `NotAction` (freeze style B relies on it);
  - the exact text of a budget notification and a budget action notification on SNS (the guard reads the budget name from them).

  Verify by linking each finding to the API call or console output that shows it, and update design D2 and D4 if any finding differs.
- [x] 1.2 Create the Go module, `guard/`, `config/`, `schema/`, `cmd/guardrail/`, a root `Makefile` that builds the function and CLI into `bin/`, an Apache-2.0 `LICENSE` and a `SECURITY.md`; verify `make` builds both binaries

## 2. Config document and validator

- [x] 2.1 Write the config JSON schema in `config/schema.json`, with the defaults from `guard-config`, and two examples (dry-run start, enforcing MSP) in `config/examples/`; verify both examples pass and a document with an approvers field fails
- [x] 2.2 Implement the validator package (schema plus the semantic rules: payer account match, duplicate scopes, payer or root as scope, threshold range); verify table tests for each rule and that messages name the offending field
- [x] 2.3 Implement the per-field diff and tightening/loosening classification, and the "stricter of applied and new" merge (design D6); verify table tests for every field in `guard-config` "Tightening applies at once", a removed scope, an added scope and a mixed version
- [x] 2.4 Document the config format and the tightening rules in `docs/config.md`; verify every field in the schema is described

## 3. Jennah contract and CLI

- [x] 3.1 Define the `config_versions` and `events` table layouts with a layout version in `schema/` (design D5, D8); verify a test that the declared columns match what the guard reads and writes
- [x] 3.2 Implement a small Jennah client on `jennah-sdk-go`: read the latest config version, insert a version, commit an event with an idempotency key, create and read an approval by request key; verify tests against a fake HTTP server for each call, including a primary-key conflict and an idempotent replay
- [ ] 3.3 Implement `guardrail setup --msp <id> --payer <account>`: create both datasets, declare tables, print the selectors and permissions for the reader and writer credentials (design D9); verify that a second run against a local Jennah reports no change
- [x] 3.4 Implement `guardrail validate`, `guardrail diff` and `guardrail publish` (design D11); verify that publish shows loosening differences, refuses an invalid document, and writes exactly one version when retried
- [x] 3.5 Implement `guardrail pull` (newest or `--version N`, applied and pending status from the inbox on stderr, design D11); verify that a pulled file republished unchanged is reported by `diff` as having no differences, and that a pending newest version is reported as pending
- [ ] 3.6 Document setup, credential minting (personal scoped keys now, service accounts later), and the pull, edit, publish flow in `docs/operating.md`; verify the commands run as written against a local Jennah

## 4. Guard: config pull and loosening

- [x] 4.1 Implement the state held in DynamoDB (applied config, pending loosening, first-seen time, frozen scopes) and the pull loop: validate, merge, apply, keep the last valid config when Jennah is unreachable or the version is invalid; verify unit tests with a fake Jennah for an invalid version, an unreachable Jennah and a restart
- [x] 4.2 Implement loosening approvals: one approval per version with a request key derived from payer account and version, summary listing the loosening differences, delay before applying, and cancellation when superseded; verify tests for approved, rejected, expired and superseded approvals and that a repeated pull raises no second approval

## 5. Guard: budgets, freeze and lift

- [x] 5.1 Implement budget reconcile with ownership by name and tag (design D1, D3): create, update, delete owned budgets and their actions; leave others alone; verify tests against a fake Budgets API that a second reconcile is a no-op and a customer budget is untouched
- [x] 5.2 Implement OU scopes: expand membership into the budget filter, attach to the OU, refresh daily; verify a test where an account joins a protected OU
- [x] 5.3 Implement dry-run (manual-approval action plus notification, design D2) and the notification handler that records `freeze` or `would_freeze` and publishes the local alert; verify handler tests for both modes
- [x] 5.4 Implement lifting: on an applied raise when actual cost is below the new threshold, and at period start for `auto_lift`, reverse the action or detach as task 1.1 decided; verify tests for a raise that lifts, a raise still too low, `auto_lift` and `stay_frozen`
- [x] 5.5 Implement the daily drift check and heartbeat; verify a test where a hand-detached policy produces one `drift` event and no re-attachment
- [x] 5.6 Give budget actions only to enforcing scopes (design D2): no action for a dry-run scope, add the action when a scope moves to enforce, and remove it when it goes back to dry-run; verify unit tests for both switches and that a dry-run budget has no action

## 6. Guard: events to Jennah

- [x] 6.1 Implement deterministic event ids, the SQS outbox producer and consumer with the event id as idempotency key, and the dead-letter alert (design D8); verify tests where a timed-out commit is retried once and the inbox holds one event, and where events queued during an outage keep their original time

## 7. CloudFormation stack

- [ ] 7.1 Write the template: both freeze policies, the Budgets execution role, the function and its role, schedules, the notification topic, the outbox queue and dead-letter queue, the local alert topic, the state parameter, secret references, the loosening approval parameters, and a delete-time custom resource that detaches freezes and removes owned budgets; verify `cfn-lint` passes and the template deploys and deletes cleanly in the test organization
- [x] 7.2 Add an install check that reports targets with no free SCP slot; verify it reports a target that already has 5 SCPs
- [ ] 7.3 Document the install, the stack parameters, the IAM policy that protects the config-source settings in the payer account, and the known gaps (service-linked role scale-out, usage of existing resources, Budgets lag), and what AWS Budgets costs per enforcing scope with the advice to prefer OU scopes; verify the doc's steps against a fresh install

## 8. Integration

- [ ] 8.1 End to end in the test organization with a local or staging Jennah: setup an MSP, publish a dry-run config, deploy, push a test account over a low ceiling, and see a `would_freeze` event; switch it to enforce and see the policy attached and a `freeze` event; publish a raise, approve it by email, and see the lift after the delay; detach by hand and see `drift`
- [ ] 8.2 Repeat the freeze with Jennah unreachable and confirm the policy attaches and the events arrive after Jennah returns
- [x] 8.3 Run `goimports`, `make` and `go test ./...`; verify all pass

## Workflow follow-up

- Agree the cost basis and the approvers per MSP with the distributor before any scope goes to enforce.
- Swap the guard's credentials to service accounts once Jennah supports them.
- Archive this change after the dry-run month.
