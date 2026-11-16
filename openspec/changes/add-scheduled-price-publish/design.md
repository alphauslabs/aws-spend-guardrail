# Design

## Context

The guard is one Lambda (`guard/main.go`) that dispatches on its input: `{"task":"pull"}` every 5 minutes, `{"task":"trail"}` every minute with the AI guard on, `{"task":"daily"}` at 00:15 UTC, plus organization events, outbox messages, and the alert topic. Each schedule is an `AWS::Scheduler::Schedule` in `guard/template.yaml`. The function has reserved concurrency 1 and a 120-second timeout. It reads its reader and writer keys from Secrets Manager at cold start. `guardrail prices publish` (`pricesCmd`, `prices.Fetch`, `cli.PublishPrices`) builds, compares, and publishes. add-manual-price-overrides makes it carry manual prices forward from the latest version, and requires a note only when manual prices change. A dry run of the build takes about 7 seconds.

## Goals / Non-Goals

**Goals:** an enterprise's shared prices stay current with no person involved; failures, changes, and newly unpriced models are reported; stacks that don't publish are unchanged.

**Non-Goals:**
- Choosing which payer org publishes, or stopping two from publishing. The operator chooses; duplicates are harmless.
- A staleness alert on every guard (it would need a record of each check, even unchanged ones). Left for later.
- Publishing chunks other than AI us-east-1.

## Decisions

**D1. A task in the guard, not a second function.** `{"task":"publish_prices"}` joins the dispatch. One package and one code hash to release and verify (docs/verify.md), one log group, one alert sink. The cost is a few read-only grants and a third secret in the publishing org only. Rejected: a separate Lambda (a second artifact to release and verify) and a GitHub Actions schedule (works only for enterprises Alphaus runs).

**D2. Stack parameters and condition.** `PublishSharedPrices` (`"false"` default) and `PricePublisherSecretArn` (required when on, by a stack rule like `AIGuardNeedsTrail`). Under condition `PricePublishing`: a `PricePublishSchedule` at `cron(45 0 * * ? *)` (daily, after the 00:15 daily run, so the two don't queue behind each other on reserved concurrency 1); a guard role statement allowing `pricing:GetProducts` and `bedrock:ListFoundationModels` (both need `Resource: "*"`) and `secretsmanager:GetSecretValue` on the publisher secret; and `PRICE_PUBLISHER_SECRET_ARN` in the environment. With the condition false none of these exist.

**D3. One publish implementation.** The logic in `cli.PublishPrices` that reads the latest version, merges manual prices, diffs, applies the note rule, and publishes moves to a function in `internal/prices` (or stays in `internal/cli` with a result value instead of printing), returning the changes, the unmatched models, and the version written. The CLI prints the result; the task alerts from it. The task passes no manual price file and no drops, so it never changes manual prices and needs no note. Its version note is "scheduled publish by guard <payer id>".

**D4. The key is read when the task runs.** The task reads the publisher secret itself and dials Jennah with it; cold start doesn't, so the other tasks never hold it. The Price List client is created for us-east-1 explicitly (its API endpoint), whatever the stack's region.

**D5. Alerts and state.** State item `prices.publish`: `{failing, unpriced: [model ids]}`.
- Failure (build, read, or publish): alert once while `failing` is empty ("guardrail: shared prices not published"), with the error line; recovery alert when a run succeeds with `failing` set.
- A version written: one alert, "guardrail: shared prices version N published", listing the changes as the CLI prints them, and saying guards apply only added and raised prices.
- Unpriced: the build's unmatched models minus manual ones; models not in the stored list are alerted in one message ("guardrail: models with no price"), then the list is replaced, so a model priced and later unpriced is reported again.

**D6. The third key in the daily check.** When `PRICE_PUBLISHER_SECRET_ARN` is set, the daily run adds `{Name: "price publisher"}` to the keys it checks. The key is dialed for the check only, and existing expiry reporting and warnings apply unchanged.

**D7. Setup output.** `schema` gains `PricePublisherCredential()`: dataset `guardrail.prices`, permissions `datastore.data:read`, `datastore.data:write`. `setup` prints it after the guard keys, with "only for the one payer org whose stack sets PublishSharedPrices".

## Risks / Trade-offs

- The publishing org's guard holds a key that can write prices for the whole enterprise. A compromise of that guard could publish inflated prices to every guard (tighten-only means overestimates, never under). Mitigations: the key reaches only `guardrail.prices`, every version is kept with its note, and the change alert reports what moved.
- A publish run shares reserved concurrency with the trail task; about 7 seconds once a day delays one trail tick slightly.
- If the publishing org's stack is deleted, publishing stops silently. Documented: move the parameter to another org first.

## Migration Plan

None. The parameter defaults off. Enabling it needs `setup` (for the printed reach), a minted publisher key in Secrets Manager, and a stack update.
