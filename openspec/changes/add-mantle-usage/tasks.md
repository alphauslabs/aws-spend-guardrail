# Tasks

## 1. Metrics reader

- [x] 1.1 Add `Calls` to `activity.Record` (1 for trail records) and the `Metric` category; the AI pack sets `Usage.Calls` from it (design D1)
- [x] 1.2 In `internal/awsx`, a CloudWatch client for one region that runs the three labeled `SEARCH` expressions of design D2 over a time window, follows `NextToken`, and returns datapoints per account, model, metric, and minute; and a call listing a sink's attached link accounts
- [x] 1.3 Add `activity.MetricsReader`: per region, read the last 15 minutes, compute growth against the counted values in meta `mantle.<region>`, emit records (design D1, D3), drop minutes older than 15 minutes, and save state only on `Commit`
- [x] 1.4 Track per-region read failures in meta `mantle.status`: alert once on the first failure with the error, again on the first success (design D5)
- [x] 1.5 Teach the engine to read both readers each tick and commit both only once usage is stored; tests: a grown datapoint counts only the growth, a failed usage write rereads and counts once, a region error alerts once and recovers, a shrunk datapoint is ignored

## 2. AI pack

- [x] 2.1 `Wants` accepts mantle metric records; `Evaluate` prices them, applies the model and region allowlists and unknown-model policy, skips the API-key tripwire, and says "through mantle" in hit details
- [x] 2.2 Price mantle records with `PriceIn(model, region)`; match allowlist entries to mantle ids up to a version suffix (design D4)
- [x] 2.3 Tests: `openai.gpt-oss-20b` priced at its own price in the record's region; allowlisting `openai.gpt-oss-20b-1:0` allows it; a model off the allowlist cuts off with the mantle detail; an unpriced model follows the unknown-model policy; 2 inferences count as 2 calls

## 3. Stack, guard, and CLI

- [x] 3.1 Template: `MantleRegions` parameter, `MANTLE_REGIONS` env, `cloudwatch:GetMetricData`, `cloudwatch:ListMetrics`, `oam:ListSinks`, and `oam:ListAttachedLinks` for the guard's role when the AI guard is on; `TEMPLATE_REVISION` and `MinTemplateRevision` to 5; template tests and cfn-lint pass
- [x] 3.2 `guard/main.go`: build a `MetricsReader` per listed region when the AI guard is on, sharing the activity store
- [x] 3.3 Daily task: for each listed region, compare the sink's attached links with the organization's active accounts (minus excluded accounts and the payer), and report each unlinked account once with a `mantle_unlinked` event and alert, tracked like `unprotected_account` (design D6); add the event kind to the inbox contract and alert text checks
- [x] 3.4 `guardrail preflight --ai`: per listed region (from a new `--mantle-regions` flag), report whether a sink exists and how many active accounts are linked, naming the unlinked ones

## 4. Docs and build

- [x] 4.1 Docs: `ai-guard.md` (how mantle is counted, setup with a sink and the organization-wide link template, delay, cost formula from design D7, and what's not covered), `install.md` (the parameter), `aws-facts.md` (the 2026-11-17 mantle facts), `upgrade.md`; CHANGELOG with Upgrade (update the stack with the new template, revision 5; optional setup for mantle), Security-relevant (the role's new CloudWatch and OAM read permissions; a new parameter), and rollback note; docs style (no em-dashes, Oxford comma)
- [x] 4.2 Run `goimports`, `make`, `go vet ./...`, `go test ./...`, and cfn-lint; verify all pass

## 5. Live check

- [x] 5.1 On e2e with the AI guard on (dry-run scope for e2enormal) and `MantleRegions=us-east-1`, with a sink in the payer and a link from e2enormal: make Chat Completions and Responses calls from e2enormal and confirm the guard's hourly totals match the calls' reported tokens and count, within about two minutes; confirm a model off the allowlist is reported as a would-cutoff naming mantle
- [x] 5.2 Delete the link and confirm the daily check reports e2enormal once as `mantle_unlinked`; delete the sink and confirm one "not watched" alert, then recreate it and confirm the recovery alert
- [x] 5.3 Remove the test setup: link, sink, `MantleRegions`, and the AI guard back off
