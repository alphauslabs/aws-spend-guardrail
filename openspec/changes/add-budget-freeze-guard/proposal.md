# Proposal

## Why

An AWS distributor that owns one payer org per MSP client needs a hard budget cap on all AWS spend per account or OU: when a ceiling is reached, new spend is frozen, while nothing already running is stopped or deleted. The cap must hold even if Jennah, which keeps the config, approvals and audit, is unreachable.

Jennah is the config and approvals backend from the first version, while the freeze itself never depends on it.

## What Changes

- **A guard per payer org**, installed by one CloudFormation stack in the payer (management) account in us-east-1. It holds two pre-created, unattached freeze SCPs (style A: deny targeted cost-creating actions; style B: deny everything except reads, cost-reducing actions and support, billing and security tooling), the execution role AWS Budgets assumes, the guard function, an outbox queue, a local SNS alert topic, and references to the guard's two Jennah credentials.
- **Freeze by AWS Budgets.** The guard reconciles AWS Budgets and Budgets actions from config: one cost budget per protected account or OU, with an action that attaches the chosen freeze SCP at the configured threshold of the ceiling. The freeze needs no call to Jennah and no guard code at the moment it fires.
- **Lifting a freeze.** A freeze lifts only when the budget is raised above current spend (a loosening, so it needs approval) or, if the scope's month-end setting allows it, when a new budget period starts. Nothing is ever stopped, terminated or deleted by the guard.
- **Dry-run first.** Every scope can be in `dry-run` (record "would freeze", attach nothing) or `enforce`. New installs and new scopes default to dry-run.
- **Config lives in Jennah.** Each MSP's config is a series of immutable, versioned documents in a dataset in the distributor's Jennah enterprise. The guard pulls the latest version on a schedule, validates it against a JSON schema shipped in this repo, and keeps the last valid config when Jennah is unreachable or a version is invalid.
- **Tighten now, loosen later.** Tightening parts of a new version apply at once. Loosening parts wait for a delay and for a Jennah approval the guard raises, decided by approvers fixed in the stack (not in Jennah). A rejected or expired approval leaves the stricter config in place.
- **Everything is recorded in Jennah.** Freezes, would-freezes, lifts, applied and rejected config versions, loosening approvals and drift (for example an SCP detached outside the guard) go to an inbox dataset, buffered in a local queue while Jennah is unreachable.
- **A `guardrail` CLI** for the distributor: set up an MSP's datasets and tables, pull a config version to edit, validate a config document, and publish it as the next version.

Non-goals for this change: the learning agent and working-limit band (phase 2), the daily Cost Explorer extract, integrations with billing systems and UI screens outside Jennah, the AI fast path, pricing create events (phase 2b), stopping or suspending running resources, Jennah service accounts (the guard takes whatever API keys it is given), config source `file`, and release signing of the function package.

## Capabilities

### New Capabilities

- `spend-freeze`: freeze SCPs, the Budgets reconciliation that attaches them at a ceiling, dry-run, lifting (by raised budget or period start), drift detection, local alerts, and the CloudFormation install in a payer account.
- `guard-config`: the config document and its schema, validation, versioning, how the guard pulls and keeps config, tightening versus loosening, loosening approvals, and the `guardrail` CLI that validates and publishes config.
- `guard-jennah-contract`: the Jennah datasets and tables the guard reads and writes, their naming per MSP, the two credentials and the reach each needs, the event records, buffering while Jennah is unreachable, and how the contract is versioned for the phase 2 agent.

### Modified Capabilities

None. This is the repo's first change.

## Impact

- **New repo** `aws-spend-guardrail` (open source, maintained by the Jennah team): `guard/` (CloudFormation template and Go function), `config/` (JSON schema and examples), `schema/` (Jennah table layouts), `cmd/guardrail` (CLI).
- **Customer AWS**: one stack per payer org in us-east-1. Uses AWS Budgets, Budgets actions, Organizations SCPs, Lambda, SQS, SNS, Secrets Manager, SSM Parameter Store, EventBridge Scheduler.
- **Jennah**: uses existing public APIs only (`data:commit`, `data:query`, schema declaration, dataset creation, approvals). No Jennah change is required. Credentials are personal scoped API keys until `add-service-accounts` (and its prerequisite `revoke-keys-on-member-removal`) land in Jennah, then two service accounts per payer org.
- **Distributor**: one Jennah enterprise; per MSP, two datasets and the roles or keys that reach them; approvers for loosening listed on Jennah's outside-approver allowlist.
- **Open with each distributor before enforcing**: whether AWS cost as measured by Budgets is the right basis rather than the price they bill their customers, and who approves loosening per MSP.
