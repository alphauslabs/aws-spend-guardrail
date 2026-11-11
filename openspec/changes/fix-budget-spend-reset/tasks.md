# Tasks

## 1. AWS adapters

- [x] 1.1 Add a `SpendSource` interface in `internal/guard` (`Actual(ctx, accounts, from, to) (float64, error)`) and a Cost Explorer implementation in `internal/awsx` using `GetCostAndUsage` with `UnblendedCost`, a `LINKED_ACCOUNT` filter, and `MONTHLY` granularity summed over the period (design D1); unit-test the request shape and summing with a fake client
- [x] 1.2 Add `Freeze(ctx, spec)` to the `Budgets` interface and `internal/awsx`: attach the spec's freeze policy to its target, treating `DuplicatePolicyAttachmentException` as success (design D3); test both outcomes

## 2. Lifts

- [x] 2.1 In `liftRaised`, keep the threshold-raised test first, then read Cost Explorer spend for the scope and lift only when it is below the new threshold; on error keep the freeze and alert once per state change; record `actual_usd` and `spend_source: cost_explorer` in the `lift` event (design D1)
- [x] 2.2 Tests: raised to 5 with 5.74 spent keeps the freeze on the raise run and on later runs where the budget reads 0; raised to 8 lifts with 5.74 recorded; a Cost Explorer error keeps the freeze and alerts once over several runs

## 3. Spend watch

- [x] 3.1 Keep each watch's `Since` and `LastChecked` as reported records in the state table (`spend_watch`, `spend_check`); record an entry in reconcile whenever `Put` creates or updates a budget (design D2)
- [x] 3.2 Add the watch step to the pull: hourly per entry; end it when the budget reports nonzero spend, after 48 hours, when the budget is gone, or at a new period; skip frozen scopes; at or over threshold, freeze with `guard_attach` in `enforce` (`MarkFrozen`, `freeze` event, local alert) or emit `would_freeze` in `dry-run`
- [x] 3.3 Set `how: budget_action` and `spend_source: budget` in `OnThreshold`'s freeze and would_freeze events (design D4)
- [x] 3.4 Tests: a lowered ceiling below Cost Explorer spend freezes on the same pull; spend crossing later freezes on the next hourly check, not sooner; nonzero budget spend, 48 hours, and a new period each end the watch; a frozen scope isn't frozen again; dry-run records `would_freeze` once; Cost Explorer is called at most once an hour per watched budget

## 4. Stack, docs, and build

- [x] 4.1 `guard/template.yaml`: add `organizations:AttachPolicy` to the freeze-policy statement (renamed `AttachDetachFreezePolicies`) and `ce:GetCostAndUsage` on `*`; wire the Cost Explorer client in `guard/main.go`; run `cfn-lint`
- [x] 4.2 Docs: in `docs/aws-facts.md`, AWS Budgets zeroes a budget's spend on create or update; in `docs/operating.md`, how lifts and the post-change watch use Cost Explorer and the event fields; in `docs/install.md`, the new permissions and the Cost Explorer cost
- [x] 4.3 `CHANGELOG.md`: Fixed (a raised but still too low ceiling could lift a freeze on a later run; a changed budget couldn't freeze until AWS recalculated), and Security-relevant (`AttachPolicy` on the freeze policies, `ce:GetCostAndUsage`, a new outbound call to Cost Explorer); no em-dashes, Oxford comma
- [x] 4.4 Run `goimports`, `make`, and `go test ./...`; verify all pass

## 5. Live check

- [ ] 5.1 With e2e on this build, lower e2estandard03's ceiling below its Cost Explorer spend and confirm the guard attaches the freeze policy within minutes with a `freeze` event recording `guard_attach`; then raise it to just under spend and confirm the freeze stays over at least two runs; then raise it above spend, approve, and confirm the `lift` event records `spend_source: cost_explorer`
