# Proposal

## Why

The budget guard freezes spend when AWS Budgets reports it over a ceiling, and Budgets lags by hours, at times a day. That is fine for a monthly budget, but a leaked AI credential burns Bedrock spend in minutes. Spend on AI models needs a cap that reacts in minutes, from the guard's own reading of what is being called, not from billing data.

Measured in a test organization (2026-11-11), the management account can see this without anything installed in member accounts: the organization CloudTrail trail records every Bedrock runtime call in the org as a free management event, with caller, model, region, exact input and output token counts, and whether a Bedrock API key was used. Its S3 files land 0.5 to 5 minutes after a call, and an attached SCP takes effect in 1 to 3 seconds. So the guard in the payer account can price each call, keep running totals, and cut off AI access within minutes.

## What Changes

- **A fast, trail-based engine in the guard, shared by packs.** The guard reads the organization trail's S3 files as they land, hands each record to the packs that care about it, keeps per-account totals over rolling windows, and runs tripwires. A pack that trips attaches its own narrow deny SCP to the account. Lifting goes through the existing loosening approval. The engine is generic so a later pack (EC2 and GPU launches, priced at launch) adds a price table, tripwires, and a deny policy, not a second guard.
- **The first pack: AI (Bedrock and AgentCore).**
  - Prices every Bedrock runtime call from its token counts and a model price table shipped with each release, matching inference profile ids and model ARNs to one model.
  - Cuts off an account when its estimated spend passes an hourly rate cap, a daily cap, or a monthly cap set in config.
  - Tripwires that cut off at once, with no estimate: a model or region not on the allowlist, a Bedrock API key used by a caller not allowed to, or a change to Bedrock's logging settings.
  - The cutoff denies Bedrock invoke calls, Bedrock API keys, and AgentCore invoke and session calls in that account, and nothing else.
- **Config gains an `ai` section:** caps per account or OU, model and region allowlists, which callers may use API keys, price overrides, mode (`dry-run` or `enforce`), and the list of cutoffs to lift. Tightening applies at once; loosening (a higher cap, an added model, a lower price, a lift) needs the existing approval and delay.
- **The inbox gains AI events:** each cutoff or would-cut-off with its evidence, each lift, and hourly usage summaries per account and model. Raw trail records stay in the client's AWS.
- **The stack gains:** an opt-in parameter, the AI deny policy, a one-minute schedule for reading the trail, read access to the trail bucket, decrypt access to the trail's KMS key when the files are encrypted, and permission to attach and detach the AI deny policy only.

Non-goals for this change: the compute (EC2 and GPU) pack, which is the next change; anything installed in member accounts (a faster, optional path for later); preventive SCPs that block models or regions before use; stopping anything already running; spend estimates for AgentCore runtime compute (models it calls through Bedrock are counted); and Bedrock Agents, knowledge bases, and flows, which are paid CloudTrail data events.

## Capabilities

### New Capabilities

- `activity-guard`: the shared fast path. Reading the organization trail, totals over rolling windows, tripwires, cutoff by a pack's deny SCP, lift, dry-run, and behavior when the trail stalls or Jennah is unreachable.
- `ai-pack`: Bedrock and AgentCore specifics. What is priced and how, the caps, the tripwires, and what the deny policy covers.

### Modified Capabilities

- `guard-config`: the `ai` section, its validation, and which of its changes tighten or loosen. Introduced by `add-budget-freeze-guard`, not archived yet, so this delta adds requirements.
- `guard-jennah-contract`: the AI event kinds and usage summaries the guard writes to the inbox.

## Impact

- **Code:** new packages for the trail reader, price tables, totals, and the AI pack under `internal/`; `config` (the `ai` section); `internal/guard` (a new task route and the cutoff and lift flow); `schema` (event kinds); `guard/main.go` (wiring).
- **Template:** `EnableAIGuard`, trail-location, and trail KMS key parameters, the AI deny SCP, a one-minute schedule, S3 read on the trail bucket, and `organizations:AttachPolicy` and `DetachPolicy` limited to the AI deny policy.
- **Client prerequisites:** an organization trail recording management events in all regions; its bucket in the management account, or a bucket policy in the log archive account granting the guard read; and for an encrypted trail, a key policy statement letting the guard decrypt (both documented).
- **Docs:** a new `docs/ai-guard.md`; `docs/config.md`, `docs/install.md`, `docs/operating.md`, and `docs/aws-facts.md`; `CHANGELOG.md` (Security-relevant: the new SCP, the attach permission, S3 read on the trail, and decrypt on its KMS key).
- **Existing installs:** unchanged unless `EnableAIGuard` is set.
