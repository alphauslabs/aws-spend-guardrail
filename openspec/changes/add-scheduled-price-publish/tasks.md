# Tasks

## 0. Order

- [ ] 0.1 Apply add-manual-price-overrides first (and archive add-shared-prices before it), since scheduled publishing must carry manual prices forward

## 1. Shared publish logic

- [ ] 1.1 Move the publish steps out of `cli.PublishPrices` into a function that returns the changes, unmatched models, and version written, without printing; keep the CLI's output identical (design D3); existing CLI tests pass unchanged, plus a test of the returned result

## 2. Guard task

- [ ] 2.1 Add the `publish_prices` task to the guard's dispatch: read the publisher secret, build with a us-east-1 Price List client, publish with no manual changes and the scheduled note (design D1, D3, D4); tests with fakes for a changed table, an unchanged one (nothing written), and manual prices carried forward
- [ ] 2.2 Keep `prices.publish` state and send the failure, recovery, version-published, and newly-unpriced alerts (design D5); tests: two failures give one alert, a success after them one recovery, a published version one change alert, an unpriced model alerted once and again after being priced then unpriced
- [ ] 2.3 Add the price publisher key to the daily expiry check when its secret is set (design D6); test that the heartbeat lists three keys and a 13-day expiry warns once

## 3. Stack and setup

- [ ] 3.1 Add `PublishSharedPrices` and `PricePublisherSecretArn`, the stack rule, the `PricePublishing` condition, the schedule, the conditional grants, and the environment variable (design D2); extend the template tests (including `TestGuardRoleGrantsCalls` for the new calls under the condition)
- [ ] 3.2 Add `PricePublisherCredential` to `schema` and print it in `setup` (design D7); update the setup CLI test

## 4. Docs and build

- [ ] 4.1 Document scheduled publishing in `docs/operating.md` (Shared AI prices: turning it on in one payer org, minting the key, the alerts, moving it) and the parameters in the stack parameter docs; add CHANGELOG entries including Security-relevant (the optional third key and grants); follow the docs' style (no em-dashes, Oxford comma)
- [ ] 4.2 Run `goimports`, `make`, `go vet ./...`, and `go test ./...`; verify all pass

## 5. Live check

- [ ] 5.1 On e2e: mint a publisher key, update the stack with `PublishSharedPrices=true`, invoke the task once by hand and confirm it writes nothing (no change); publish a lowered test price by CLI, invoke again and confirm it publishes the Price List's price with one change alert; set the secret to a revoked key and confirm one failure alert; restore and remove the test setup
