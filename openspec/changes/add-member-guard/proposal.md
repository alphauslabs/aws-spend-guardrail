# Proposal

## Why

The guard caps AI spend per account from the payer org, so one busy or compromised caller cuts off everyone in its account, and an MSP's client has no way to budget its own people and workloads. Bedrock has no native per-caller cap, and AWS Budgets filtered by principal lags by about a day. This change adds the member guard: a separate guard that a client installs in one of its own member accounts to cap AI spend per caller (an IAM role, a role session, an IAM user, or a Bedrock API key) within a minute or so. It is sold by the MSP and configured by the client, and it lives in this repo so it shares the guard's pricing, trail parsing, and Jennah client. It has its own releases.

The payer-level attempt (`add-caller-caps`) is replaced by this change: per-caller caps belong to the client, while the payer guard stays the backstop the client can't turn off.

## What Changes

- **New member guard** (`member/`): a Lambda function and CloudFormation template installed in one member account. It reads the account's own Bedrock calls from CloudTrail events on that account's EventBridge bus (one forwarding rule per region to a home region), prices them with the guard's AI price tables and shared prices, and keeps totals per named caller over hourly, daily, and monthly windows.
- **Caller caps**: config lists callers by `role` or `user` name, with USD caps per window. A role entry can set `per_session: true` so each session name (for example, one per developer) gets its own budget.
- **Cutoffs with IAM, not SCPs**: a cut-off caller gets a deny policy for AI actions, written by the member guard under its own IAM path and attached to that role or user, narrowed to session ids when the cap is per session. Other callers keep working. A cutoff lifts by itself when the window that tripped it resets, or earlier through an approved loosening.
- **Limits, stated plainly**: IAM Identity Center (SSO) roles can't be changed from a member account, so SSO callers are counted and alerted (`would_cutoff`) but not denied. `bedrock-mantle` usage carries no caller and doesn't count toward caller caps. The client's own admins can remove the member guard or its policies. Account-wide caps remain the payer guard's job.
- **Config and audit in Jennah**: each member guard reads its config from a Jennah dataset and writes its events to an inbox dataset, both under the MSP's payer org: `guardrail.<msp>.<payer>.<account>.config` and `.inbox`. Tightening applies at once. Loosening needs approval from the client's approvers. The guard keeps working on its last valid config when Jennah is down.
- **Independent releases**: the member guard is released from tags `member-vX.Y.Z` by its own workflow, with its own changelog (`member/CHANGELOG.md`), template, function package, and `guardrail-member` CLI. Guard releases (`vX.Y.Z`) are unchanged and don't include the member guard. Member releases keep the cheap gates (tag on `main`, pinned actions, vulnerability check, reproducible build, signed checksums, a changelog entry with a Security-relevant section), and drop state fixtures, the rollback check, the required Upgrade section, and the SBOM.
- The guard CLI's release lookup ignores member releases, and member releases are never marked as GitHub's latest release, so `guardrail versions` keeps comparing against guard releases.
- The parked `add-caller-caps` change is removed. Its caller normalization and `principalId` parsing from `wip/add-caller-caps` are reused.

## Capabilities

### New Capabilities

- `member-guard`: what the member guard counts, how it attributes calls to callers, caps per caller and per session, how it cuts off and lifts, and its stated limits (SSO, mantle, the client's own admins).
- `member-config`: the member guard's config document, its validation, and which changes are tightening or loosening.
- `member-jennah-contract`: the member guard's datasets, credentials, events, heartbeat, and approvals in Jennah.
- `member-releases`: how member releases are tagged, built, gated, and published, independently of guard releases.

### Modified Capabilities

- `guard-upgrades`: the latest guard release that `guardrail versions` compares against is found among `vX.Y.Z` tags only, ignoring member releases.

## Impact

- New: `member/` (function `main.go`, `template.yaml`, `CHANGELOG.md`), `cmd/guardrail-member`, `internal/member` (engine, IAM deny writer, config pull), `config/member.go` and its schema, `.github/workflows/release-member.yml`, `docs/member-guard.md`.
- Shared and touched: `internal/activity/record.go` (`principalId`, parsing one EventBridge record), `internal/packs/ai` (caller helper), `internal/awsx` (per-caller totals and IAM writes), `internal/jennah` and `schema/` (member dataset ids and tables), `internal/releases` (list releases and keep `vX.Y.Z` only), `Makefile` (`release-member` target), `tools/changelogcheck` (a changelog path and tag prefix), `.github/workflows/ci.yml` (lint the member template).
- Removed: `openspec/changes/add-caller-caps/`.
- Jennah (follow-ups in the jennah repo): the console's guardrail pages show member guards and their caller cutoffs, and its latest-release check ignores `member-v*` tags.
- AWS cost to the client: no second trail when the org already has a trail; EventBridge rules, one SQS queue, one Lambda function, and one DynamoDB table in the member account.
