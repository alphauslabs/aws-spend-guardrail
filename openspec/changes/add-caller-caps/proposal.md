# Proposal

## Why

The AI guard caps spend per account, so one busy or compromised workload cuts off every other AI user in its account. Bedrock has no native per-role spend cap, and AWS Budgets filtered by principal lags by hours. The guard already reads each call's caller from the trail, so it can cap a single role, IAM user, or Bedrock API key and deny only that caller, within minutes.

## What Changes

- An AI scope (account or OU) can list `callers`: each names an IAM role or IAM user and sets hourly, daily, or monthly caps in USD. In an OU scope, each caller entry applies in each member account separately: one budget per caller per account.
- Each priced call from the trail is attributed to its caller: an assumed-role session counts toward its role, and an IAM user (including the `BedrockAPIKey_*` user behind a long-term Bedrock API key) counts as itself. Short-term API keys count toward the role that minted them.
- The guard keeps per-caller totals over the same windows as account totals, for named callers only.
- When a named caller passes a cap in an `enforce` scope, the guard denies AI access to that caller only, through a new caller cutoff SCP on the organization root that matches the caller with `aws:PrincipalArn`. In `dry-run` it records and alerts what it would have cut off. The account's other callers keep working.
- If a caller cutoff can't fit in the caller cutoff policy (AWS's 5,120-character limit), the guard cuts off the whole account instead, and alerts once.
- Caller cutoffs have their own cutoff ids, lifted through `ai.lifts` like account cutoffs, or automatically when the caller's cap is raised or removed by an approved change.
- Adding a caller entry or lowering a caller cap is a tightening and applies at once. Raising or removing a caller cap, or removing a caller entry, is a loosening and needs approval and the delay.
- Validation: a caller entry names exactly one of `role` or `user`, a caller cap can't exceed its scope's cap for the same window, and a caller is named once per scope.
- Usage through the `bedrock-mantle` endpoint can't be attributed to a caller (its metrics carry only account and model), so it doesn't count toward caller caps; it still counts toward the account's caps, and a caller cutoff denies that caller on both endpoints. Documented as a limit.
- Tripwires (unknown model, API key not allowed, and the rest) still cut off the whole account.
- The stack gains the caller cutoff policy and the guard's permission to write and attach it. Template revision 9; the guard requires it.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `ai-pack`: caps per caller, the caller cutoff and what it denies, the caller cutoff policy's size and drift handling, and mantle usage not counting toward caller caps.
- `activity-guard`: totals kept per named caller as well as per account.
- `guard-config`: the `callers` field of an AI scope and its validation; caller cap changes classified as tightening or loosening.

## Impact

- `config/ai.go`, `config/aidiff.go`, `config/schema.json`: the `callers` field, validation, classification, and the stricter merge.
- `internal/packs/ai`: caller normalization (shared with `api_key_callers` matching).
- `internal/activity/engine.go`, `internal/awsx/activity.go`: per-caller totals (`usage#ai#<account>#<caller>`), caller cutoff records, caller cutoff ids.
- `internal/guard`: a caller cutoff policy reconciler beside the block policy, daily drift check, and the fallback to the account cutoff.
- `guard/template.yaml`, `guard/main.go`, `internal/guard/deployment.go`: the new SCP, IAM permissions, environment, and revision 9.
- `docs/ai-guard.md`, `docs/operating.md`, `docs/install.md`, CHANGELOG: config, limits, the third root SCP.
- The Jennah console: caller cutoff events should show the caller (follow-up in the jennah repo).
