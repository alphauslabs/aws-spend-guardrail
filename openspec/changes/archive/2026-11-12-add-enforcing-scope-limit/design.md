# Design

## Context

- `config.Parse` runs the schema check, then `checkSemantics`, for both the CLI and the guard. Semantic checks today cover the payer account, duplicate scopes, the organization root, exclusions, and alert emails. Mode is resolved per scope from the scope, then `defaults`, then the built-in default (`dry-run`), in `Document.Resolve`.
- In `Reconcile`, each wanted budget goes through `Budgets.Put`, which creates or updates the budget, then creates, updates, or deletes its action (`internal/awsx/budgets.go`). A failure is collected per budget, and one "reconcile incomplete" alert carries all of them, every run. A budget whose action failed reads back as not enforcing, so the next run tries again.
- The guard already has an alert-once mechanism: `State.MarkReported`, `Reported`, and `ClearReported` by kind and name, used for carried freezes (`internal/guard/carry.go`).
- `guardrail preflight` parses the document and checks SCP slots per target with the operator's AWS credentials (`cmd/guardrail/main.go`).
- AWS documents 100 budget actions per account, not listed as adjustable (`docs/aws-facts.md`). The Budgets SDK has both `CreationLimitExceededException` and `ServiceQuotaExceededException`, and the API reference doesn't say which `CreateBudgetAction` returns at this limit.

## Goals / Non-Goals

**Goals:**
- Refuse an over-limit document where it's written, with a message that says what to change.
- Let an installer see room taken by other budget actions before installing.
- Make a refused action read as an unprotected scope, once, instead of a repeated generic failure.

**Non-Goals:**
- Splitting scopes across accounts or budgets to get past the limit.
- Tracking the limit if AWS raises it: the constant changes with a release.
- Ordering which enforcing scopes get actions first when the limit is hit.

## Decisions

### D1. Count enforcing scopes after defaults, in `checkSemantics`
The check resolves the document (`Resolve`) and counts scopes with mode `enforce`, so `defaults.mode: enforce` on 150 scopes is caught as well as 150 explicit ones. It reports one problem on `scopes`: "151 scopes enforce, and AWS allows at most 100 budget actions in the payer account, so at most 100 scopes can enforce. Group accounts into OU scopes, or set some scopes to dry-run." `MaxEnforcingScopes = 100` is exported so preflight and docs tests use the same value.

*Alternative:* put the check in the JSON schema. Rejected: the schema can't see defaults, and its messages are generic.

### D2. The validator checks the document, not the stricter config
A pending loosening can leave the guard enforcing the union of two versions (`config.Stricter`), which can exceed 100 even when both versions pass. The CLI can't see what is applied, and refusing a valid version because of what's pending would block the very change that removes the old scopes. So the document check stays per version, and the guard reports the refusal at run time (D4).

### D3. Preflight counts actions outside the guard
Preflight lists the payer account's actions with `DescribeBudgetActionsForAccount` and skips budgets whose name starts with the guard's prefix (`guardrail-`), since those are the document's own scopes or ones the guard will remove. It prints "budget actions: N outside the guard, M enforcing scopes in this config, limit 100" and fails when N + M > 100. The operator running preflight needs `budgets:DescribeBudgetActionsForAccount`; `docs/install.md` lists it with the other preflight permissions.

### D4. The guard names the limit and alerts once
`Budgets.Put` returns a sentinel (`ErrBudgetActionLimit`) wrapping AWS's error when `CreateBudgetAction` fails with either `ServiceQuotaExceededException` or `CreationLimitExceededException`, since AWS doesn't document which one applies. `Reconcile` collects these scopes apart from other failures. For each newly refused scope it marks `action_limit` as reported and sends one alert naming the scopes, saying they can't freeze until they have an action, that the payer account is at AWS's limit of 100, and the remedy, with AWS's text on an `Error:` line as `internal/alerttext` requires. A scope that gets its action, or leaves the config or `enforce`, is cleared, so a later recurrence alerts again. Other failures keep the existing "reconcile incomplete" alert.

*Alternative:* count actions before calling AWS and skip the call. Rejected: it adds a list call every run and can't be more accurate than AWS's own answer.

## Risks / Trade-offs

- [AWS returns a third error type, or a generic one, at the limit] → The refusal still shows up in the "reconcile incomplete" alert as today. A live test against the real limit costs about $10 a day (98 billable action budgets), so the unit tests use a fake; the error seen in a real refusal gets recorded in `docs/aws-facts.md` when it happens.
- [AWS raises the limit] → Documents within a higher limit are refused until the constant is updated. `docs/aws-facts.md` records the date the value was checked.
- [Valid configs that pass today are refused after upgrade] → Only configs with over 100 enforcing scopes, which already can't work: their extra scopes have no action. The guard keeps the config it has, so nothing is unprotected that wasn't already.

## Migration Plan

Ship in a guard release and a CLI release together. A guard on the new release that pulls an over-limit version it previously applied keeps that applied config (an invalid version never replaces the config in force) and reports the refused actions per D4. No stack change.
