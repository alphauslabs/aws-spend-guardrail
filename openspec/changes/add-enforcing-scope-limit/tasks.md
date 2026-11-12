# Tasks

## 1. Validator

- [ ] 1.1 Add `MaxEnforcingScopes = 100` and the enforcing-scope count to `checkSemantics` in `config/config.go`, counting resolved scopes in `enforce` and reporting one problem on `scopes` with the count, the limit, and the remedy (design D1); verify with `go test ./config/`
- [ ] 1.2 Tests in `config/config_test.go`: 101 scopes enforcing through `defaults.mode` is refused with the message; 100 enforcing and 50 dry-run is valid; 101 explicit enforcing scopes is refused; the guard's pull of such a version keeps the applied config (a test in `internal/guard` with a fake Jennah)

## 2. Preflight

- [ ] 2.1 Add a `BudgetActionsOutside(ctx, prefix)` reader to `internal/awsx` that pages `DescribeBudgetActionsForAccount` and counts actions on budgets not named with the guard's prefix; test it with a fake that returns two pages and a mix of names
- [ ] 2.2 In `preflight`, print "budget actions: N outside the guard, M enforcing scopes in this config, limit 100" and fail when N + M > 100 (design D3); test passing and failing counts

## 3. Guard

- [ ] 3.1 In `Budgets.Put`, wrap `ServiceQuotaExceededException` and `CreationLimitExceededException` from `CreateBudgetAction` in `ErrBudgetActionLimit` (design D4); test both error types and that other errors aren't wrapped
- [ ] 3.2 In `Reconcile`, collect limit-refused scopes apart from other failures, alert once per newly refused scope with the `action_limit` reported kind, and clear it when the scope gets its action, leaves the config, or leaves `enforce`; the alert passes `alerttext.Check`
- [ ] 3.3 Tests with the fake Budgets API capped at a given number of actions: the 101st scope gets one alert over three runs, its budget exists, and it gets its action once an action is freed; a pending loosening that swaps 10 scopes reports the 10 and creates them after approval removes the old ones

## 4. Docs and build

- [ ] 4.1 Document the rule in `docs/config.md` (validation), the preflight line and its `budgets:DescribeBudgetActionsForAccount` permission in `docs/install.md`, the alert in `docs/operating.md`, and add a CHANGELOG entry; verify the docs name the same limit and remedy as the messages
- [ ] 4.2 Run `gofmt`, `go vet ./...`, `go test ./...`, and `cfn-lint guard/template.yaml`; verify all pass

## 5. Live check

- [ ] 5.1 With the e2e payer, run `guardrail preflight` on the e2e config and confirm the budget action line shows the actions outside the guard (none expected) and 1 enforcing scope; then `guardrail validate` a copy with 101 enforcing account scopes and confirm the refusal message

## Workflow follow-up

- Release the guard and CLI together, then deploy the guard to e2e.
- Archive the change.
