# Proposal

## Why

AWS allows at most 100 budget actions per account, and the guard creates one action, in the payer account, for every scope in `enforce`. Today nothing checks this before the guard runs: a config with a 101st enforcing scope validates and publishes cleanly, and the guard then fails to create that scope's action on every run, with a generic "reconcile incomplete" alert carrying AWS's error. The scope looks protected in the config but can't freeze. The limit was confirmed from AWS's quotas page on 2026-11-12 (`docs/aws-facts.md`), and the install docs now state it, but the tools should enforce it.

## What Changes

- The config validator refuses a document whose scopes resolve to more than 100 enforcing scopes, counted after defaults are applied. The message gives the count and the limit, and says to group accounts into OU scopes or set some scopes to `dry-run`. Because the CLI and the guard share the validator, `guardrail validate`, `diff`, and `publish` refuse it, and a guard pulling such a version keeps the config it has.
- `guardrail preflight` also counts the budget actions already in the payer account that the guard doesn't own, and fails when those plus the document's enforcing scopes exceed 100.
- When AWS refuses to create a budget action because the account is at its limit, the guard's alert says so in plain words: which scope has no action, so it can't freeze, and what to do. This covers what the validator can't see: budget actions the guard doesn't own, and the stricter config the guard keeps while a loosening waits for approval, which can hold more enforcing scopes than either version.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `guard-config`: validation also refuses more enforcing scopes than AWS allows budget actions. (The capability's requirements are still in the in-flight `add-budget-freeze-guard` change; this change adds a requirement to it.)
- `spend-freeze`: preflight counts budget actions in the payer account, and the guard reports a refused action as a scope that can't freeze.

## Impact

- `config/config.go`: a new semantic check and a `MaxEnforcingScopes` constant (100), with tests in `config/config_test.go`.
- `cmd/guardrail/main.go` (`preflight`): reads the payer account's budget actions with the operator's credentials (`budgets:DescribeBudgetActionsForAccount`).
- `internal/awsx/budgets.go` and `internal/guard/reconcile.go`: recognize AWS's limit error on `CreateBudgetAction` and name it in the alert.
- Docs: `docs/config.md` (validation rules), `docs/install.md` (preflight), `docs/operating.md` (the alert).
- No change to the stack, the guard's IAM, events, or Jennah.
