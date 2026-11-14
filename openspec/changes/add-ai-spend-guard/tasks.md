# Tasks

## 1. Facts

- [x] 1.1 In the test organization, with a temporary organization trail: make AgentCore invoke and session calls and record their CloudTrail event names, category (management or data), and fields; confirm `PutModelInvocationLoggingConfiguration` and `DeleteModelInvocationLoggingConfiguration` names; record the results, and the facts measured on 2026-11-11 (token fields, API-key flag, S3 delivery delay, SCP attach time, no EventBridge in the management account, trails can't be narrowed), in `docs/aws-facts.md`; remove the temporary trail

## 2. Config

- [x] 2.1 Add the `ai` section to `config/schema.json`, `Document`, and `Resolve` (scopes per account or OU with hourly, daily, and monthly caps and mode; defaults; model and region allowlists; API-key callers; price overrides; lifts), with validation (design D6); verify schema and semantic tests for a minimal section, each refusal, and the Go and JSON schema agreeing
- [x] 2.2 Classify AI changes in `config/diff.go`, keep loosening AI changes out of `Stricter`, and compare them in `Equal`; verify diff tests for each tightening and loosening listed in the spec

## 3. Engine

- [x] 3.1 Trail reader: list new objects per prefix since a saved position, read each gzipped file once, and pass matching records to registered packs (design D1, D2); verify tests with a fake S3 for new files, a restart mid-batch, files from several accounts and regions, and a malformed file (skipped and alerted, position advanced)
- [x] 3.2 Totals: minute, day, and month counters per account and pack in the state table, with atomic adds and TTL, and hourly sums (design D3); verify tests for a late file counted at event time and the hourly window rolling
- [x] 3.3 Cutoff and lift: attach a pack's policy to an account, record the cutoff with id and evidence, alert, honour dry-run, and detach on an applied lift or a raised cap (design D5, D6); verify tests for enforce, dry-run, a second breach of an already cut-off account, an approved lift, a lift of an unknown id, and a lift while still in breach
- [x] 3.4 Stalled-trail detection: alert and record after 30 minutes without a new file, or when reads fail with an access error (naming it), and again on resume; verify tests

## 4. AI pack

- [x] 4.1 Price table `packs/ai/prices.json` with source and date for each model, and model matching from model id, inference profile id, and ARN (design D4); verify tests for the id forms seen in the test organization and for an override
- [x] 4.2 Matchers and pricing for the four runtime calls, using completion records for streaming calls and skipping refused calls; caps per account through scopes and defaults; verify tests from recorded trail records (non-streaming, streaming, refused, API key)
- [x] 4.3 Tripwires: model or region not allowlisted, API-key use by a caller not allowed, unknown model, and logging-configuration changes; verify a test for each
- [x] 4.4 Hourly `ai_usage` summaries and the AI event kinds in `schema` (design D7); verify tests for summary content and that no raw record or prompt is written

## 5. Stack

- [x] 5.1 In `guard/template.yaml`, under `EnableAIGuard`: the AI deny SCP (design D5), the one-minute trail schedule, S3 read on `TrailBucket` and `TrailPrefix`, `kms:Decrypt` on `TrailKmsKeyArn` when set, limited by `kms:ViaService` (design D9), and AttachPolicy and DetachPolicy limited to the AI deny policy and organization accounts; wire the `trail` task in `guard/main.go`; verify the template validates with the parameter on and off
- [x] 5.2 An install check (`guardrail preflight --ai`) that reads the organization trail's configuration and fails unless it is multi-region, logs management events, and writes to `TrailBucket`; when the trail uses a KMS key, fails unless `TrailKmsKeyArn` names it; and reads and decrypts one recent file to prove access; verify tests

## 6. Docs

- [x] 6.1 Write `docs/ai-guard.md` (what it watches and how fast, caps and tripwires, the deny policy, dry-run first, lifting a cutoff, the price table and overrides, costs, and the bucket policy and KMS key policy statements for a trail bucket or key in another account); update `docs/config.md`, `docs/install.md`, and `docs/operating.md`; follow the docs' style (no em-dashes, Oxford comma)
- [x] 6.2 Add a `CHANGELOG.md` Unreleased entry, with Security-relevant listing the AI deny SCP, the attach and detach permission on it, S3 read on the trail bucket, and decrypt on the trail's KMS key

## 7. Build and verify

- [x] 7.1 Run `goimports`, `make`, and `go test ./...`; verify all pass
- [x] 7.1a Grant the guard role `organizations:DescribeOrganization` in the AI guard's policy (the live test found the AI guard couldn't start without it: preflight runs with the installer's credentials, so it didn't catch this); add a test that every Organizations call in the guard's code is granted to the guard role; verify the test fails without the grant
- [ ] 7.2 In the test organization, with a temporary organization trail: enable the AI guard in dry-run with a $0.01 hourly cap on one member account, make Bedrock calls, and confirm the would-cut-off arrives within about 6 minutes with correct tokens and price; switch to enforce and confirm the account's Bedrock calls are refused and its other calls are not; publish a lift, approve it, and confirm Bedrock works again after the delay; trip the API-key and unknown-region tripwires; measure the listing cost per minute; then restore the stack and remove the temporary trail
