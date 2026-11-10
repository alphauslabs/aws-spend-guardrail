# Proposal

## Why

The guard protects only the accounts its config names, directly or through an OU scope. An account created in, or invited to, the payer org outside those scopes gets no budget and can't be frozen, and today nobody is told. For a distributor whose MSPs add end-customer accounts every month, that silently leaves new spend uncapped.

## What Changes

- **A daily coverage check.** The guard's daily run lists every active account in the organization and compares it with what the applied config covers: accounts named as scopes, and every account in a scoped OU.
- **Unprotected accounts are reported once.** Each account that nothing covers produces an `unprotected_account` event in the Jennah inbox and a local alert, the first time it's seen. It is reported again only if it becomes uncovered again after being covered or excluded.
- **Intentional exclusions.** A new optional config field, `excluded_accounts`, lists accounts left out on purpose (an SSO or shared-services account, for example). They are never reported. The payer account is always excluded.
- **Stale scopes.** A scope naming an account that is no longer an active member of the organization produces a `stale_scope` event and a local alert, once.
- **Faster, optional: react to account events.** A stack parameter, off by default, adds an EventBridge rule on the organization's account creation, invitation, and move events that runs the same check within minutes. It needs the account's CloudTrail management events to reach EventBridge, which some organizations don't have.

The guard only detects and reports. Deciding a ceiling for a new account and asking the MSP stays with a person today, and later with the learning agent in Jennah.

Non-goals: protecting new accounts automatically (a default ceiling for every unscoped account is a possible later change, with a cost per budget), the learning agent's handling of the event, and accounts outside the payer org.

## Capabilities

### New Capabilities

- `account-coverage`: how the guard checks that every active account in the organization is either protected by a scope or excluded on purpose, how it reports the ones that aren't, and the config field for exclusions.

### Modified Capabilities

None. `spend-freeze`, `guard-config` and `guard-jennah-contract` are still in `add-budget-freeze-guard` and not archived; this change adds its config field and event kinds under `account-coverage` instead.

## Impact

- **Guard function:** one more step in the daily run, and a handler for the optional account events. No new function.
- **Config:** `excluded_accounts` in `config/schema.json`, the validator, `docs/config.md`, and the examples. Existing documents stay valid.
- **Jennah inbox:** two new event kinds, `unprotected_account` and `stale_scope`. Additive, so the table layout doesn't change.
- **Template:** `organizations:ListAccounts` for the guard's role; an optional EventBridge rule behind a new parameter, `ReactToAccountEvents`.
- **State:** one DynamoDB item per reported account, so each is reported once.
- **Depends on `add-budget-freeze-guard`** for the guard, its config and its daily run.
