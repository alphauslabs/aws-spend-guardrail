# Tasks

## 0. Order

- [x] 0.1 Apply add-manual-price-overrides first (and archive add-shared-prices before it), since scheduled publishing must carry manual prices forward
- [ ] 0.2 Archive add-manual-price-overrides before archiving this change, since this change modifies its "Manual prices carry forward until dropped" requirement

## 1. Shared publish logic

- [ ] 1.1 Move the publish steps out of `cli.PublishPrices` into a function that returns the changes, unmatched models, and version written, without printing; keep the CLI's output identical (design D3); existing CLI tests pass unchanged, plus a test of the returned result
- [ ] 1.2 In the shared logic, drop a carried-forward manual price the build prices at or above on both sides, without needing a note, appending the drop to the version note and reporting it; return manual prices above the Price List with both prices (design D8, D9); tests: Price List equal on both sides (dropped, published prices unchanged, note appended, no `--note` needed), above on one side and equal on the other (dropped), below on one side (kept and returned as stale), a model given in this publish's file (kept), and the CLI line for each

## 2. Guard task

- [ ] 2.1 Add the `publish_prices` task to the guard's dispatch: read the publisher secret, build with a us-east-1 Price List client, publish with no manual changes and the scheduled note (design D1, D3, D4); tests with fakes for a changed table, an unchanged one (nothing written), and manual prices carried forward
- [ ] 2.2 Keep `prices.publish` state and send the failure, recovery, version-published, newly-unpriced, and stale-manual alerts (design D5, D9); tests: two failures give one alert, a success after them one recovery, a published version one change alert, an unpriced model alerted once and again after being priced then unpriced, a stale manual price alerted once and again after its prices change or after it leaves and returns
- [ ] 2.3 Add the price publisher key to the daily expiry check when its secret is set (design D6); test that the heartbeat lists three keys and a 13-day expiry warns once

## 3. Stack and setup

- [ ] 3.1 Add `PublishSharedPrices` and `PricePublisherSecretArn`, the stack rule, the `PricePublishing` condition, the schedule, the conditional grants, and the environment variable (design D2); extend the template tests (including `TestGuardRoleGrantsCalls` for the new calls under the condition)
- [ ] 3.2 Add `PricePublisherCredential` to `schema` and print it in `setup` (design D7); update the setup CLI test

## 4. Docs and build

- [ ] 4.1 Document scheduled publishing in `docs/operating.md` (Shared AI prices: turning it on in one payer org, minting the key, the alerts, moving it) and the parameters in the stack parameter docs; add CHANGELOG entries including Security-relevant (the optional third key and grants); follow the docs' style (no em-dashes, Oxford comma)
- [ ] 4.2 Run `goimports`, `make`, `go vet ./...`, and `go test ./...`; verify all pass

## 5. Live check

- [ ] 5.1 On e2e: mint a publisher key, update the stack with `PublishSharedPrices=true`, invoke the task once by hand and confirm it writes nothing (no change); publish a lowered test price by CLI, invoke again and confirm it publishes the Price List's price with one change alert; publish a manual price below the Price List for a priced model by CLI (note), invoke again and confirm one stale-manual alert; drop it, publish a manual price equal to a Price List price, invoke again and confirm it is dropped with the note line and no price change; set the secret to a revoked key and confirm one failure alert; restore and remove the test setup
