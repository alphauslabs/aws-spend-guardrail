# Design

## Context

See proposal.md for why. The repo is new and empty. Constraints that shape the approach:

- **AWS.** Service control policies only bind member accounts, never the management account, and only inside one organization, so each payer org needs its own guard. Budgets actions that apply SCPs run from the management account. Budgets is a global service served from us-east-1.
- **Jennah** (from its public API and docs):
  - Datasets are `(EnterpriseId, DatasetId)`-scoped. Ids use lowercase letters, digits, and `.`, `_`, `-` (not at the edges), up to 128 characters, with `.` as the selector hierarchy separator.
  - Data goes through `POST /v1/datasets/:id/data:commit` and `data:query` only (structured, no SQL). Commits accept an idempotency key, and a duplicate primary key is reported as a conflict.
  - Column types include `INT64`, `STRING`, `BOOL`, `TIMESTAMP` and `JSON`.
  - Approvals carry a payload and a `request_key` for idempotent creation. A key-authenticated caller can create approvals but never decide them. Expiry fails closed.
  - API keys today carry their own permission scope and dataset selectors. If Jennah adds service accounts with role-based reach, their keys may not carry selectors of their own.
- **Team**: Go everywhere on the Jennah side, so the function, CLI and validator are Go.

## Goals / Non-Goals

**Goals:**
- The freeze path is AWS-only: Budgets attaches the SCP. The guard function and Jennah only set things up and record.
- One validator, used by the CLI and the guard.
- The guard is small enough for the distributor to review before they deploy it through their own pipeline.

**Non-Goals:**
- A StackSet or multi-org deployment from one place. Each payer org gets the same template through the distributor's pipeline.
- Running the guard's compute in a linked account. Everything installs in the management account. A later change can split it into an org stack (SCPs, budgets, Budgets role, notification topic, a cross-account role) that stays in the management account and a guard stack (Lambda, queues, state, secrets) in a chosen linked account, which must then never be a freeze scope.
- Converting between AWS cost and the price a distributor bills its customers.
- Guard-side approval UI. Approvers use Jennah's existing email approvals.

## Decisions

### D1. Budgets actions do the freeze; the guard only reconciles
The guard turns config into one cost budget per scope and one action per budget (`APPLY_SCP_POLICY`, threshold as a percentage of the budget, `AUTOMATIC` approval model in enforce mode). Budgets evaluates spend and attaches the policy itself.

*Alternative:* the guard watches spend and attaches the SCP. Rejected: it puts guard code and its failure modes on the freeze path, and Budgets already does this.

### D2. Dry-run as a manual-approval action plus a notification
A dry-run scope gets the same budget and an action with the `MANUAL` approval model, so the action goes into a pending state instead of executing. A budget notification at the same threshold goes to an SNS topic the guard subscribes to, and the guard records `would_freeze`. Switching to enforce changes only the approval model, so the dry-run tests the exact budget and threshold that will enforce.

*Alternative:* dry-run budgets with notifications only and no action. Rejected: the action itself (its role, policy and target) would first be exercised on the day it goes live.

### D3. Ownership by name prefix and tag
Guard-owned budgets are named `guardrail-<scope id>` and tagged `guardrail:owned=true`. Reconcile lists budgets, considers only owned ones, and diffs them against the applied config. Customer budgets are never in the diff.

### D4. Two policies, attached by Budgets or by the guard
Style A is a deny list of cost-creating actions (the list lives in the template and is versioned with the repo). Style B is `Deny` with `NotAction` over reads (`*:Describe*`, `*:Get*`, `*:List*`), cost-reducing actions (`*:Stop*`, `*:Terminate*`, `*:Delete*`) and the support, billing and security services. Both are created unattached by the stack.

Lifting uses `ExecuteBudgetAction` with `REVERSE_BUDGET_ACTION` where Budgets supports it, so Budgets' own action status stays accurate. If reversal is not available in the needed state, the guard detaches the policy directly and records which path it used. Task 1.1 settles this.

### D5. Config as immutable JSON documents
The config dataset has one table, `config_versions (version INT64 PK, document JSON, published_at TIMESTAMP, note STRING, layout_version INT64)`. A new version is a plain insert: a duplicate version is a primary-key conflict, which gives single-writer semantics without locks. The guard reads the highest version with one `data:query` ordered by `version DESC LIMIT 1`.

*Alternative:* one table per concept (scopes, defaults) edited in place. Rejected: no history, no atomic version, and "what changed" would need its own audit table. Whole documents also give phase 2 the budget revision history it needs.

### D6. Effective config = stricter of applied and desired, until approved
The guard keeps its state in a DynamoDB table in the payer account: the applied version and the effective config, the pending loosening version (if any) with its approval id and when it was first seen, and one item per frozen scope. (An SSM parameter was the first idea, but its 4 to 8 KB limit holds only a few dozen scopes, and one item per frozen scope means the notification handler and the scheduled pull never write the same item.) On each pull:

1. Validate the newest version. If invalid: record `config_rejected`, keep the applied config.
2. Diff it against the applied config scope by scope, classifying each field as tightening or loosening (rules in `guard-config`).
3. Build the effective config: for each field, the stricter of applied and new. Apply it at once.
4. If anything loosens, create (or find, by request key) the approval. Once it is `APPROVED` and the delay has passed, apply the new version as is.

Comparing per field means a mixed version never delays its tightening parts.

### D7. Approvers live in the stack
`LooseningApprovers` (emails), `LooseningRequiredApprovals`, `LooseningApprovalWindow` and `LooseningDelay` are stack parameters. Changing them takes a stack update in the payer account, which is outside what a Jennah credential can reach. The approvers must be members of the distributor's enterprise or on its outside-approver allowlist. The guard raises the approval with its reader credential, so the requester is a key, which can never decide.

### D8. Inbox events with deterministic ids and an SQS outbox
The inbox dataset has `events (event_id STRING PK, occurred_at TIMESTAMP, kind STRING, scope STRING, config_version INT64, guard_version STRING, detail JSON, layout_version INT64)`. Event ids are derived from what happened (for example `freeze:<scope>:<budget period>:<action execution time>`) so the same occurrence always produces the same id. Every event goes through an SQS queue: the producer enqueues, and a consumer commits to Jennah with `event_id` as the idempotency key. A dead-letter queue after the retention period triggers a local alert.

*Alternative:* write to Jennah directly and queue only on failure. Rejected: two code paths, and a crash between failure and enqueue loses the event.

### D9. Two credentials in Secrets Manager
`guardrail/<msp>/reader` and `guardrail/<msp>/writer` in Secrets Manager. Reader: `datastore.data:read` and `datastore.schema:read` on `guardrail.<msp>.config`, plus `approval.requests:create`, `approval.requests:read` and `approval.requests:cancel` (to cancel a superseded loosening approval). Writer: `datastore.data:write` on `guardrail.<msp>.inbox`. Today these are personal API keys with their own scope and selectors. If Jennah adds service accounts, they can become one service account each, with a custom role per MSP. The guard doesn't care which; it only needs the secret.

### D10. One Go function, several triggers
One Lambda (Go, `provided.al2023`, arm64) handles: the 5-minute schedule (pull config, reconcile), the budget notification topic (record freeze or would-freeze), the daily schedule (OU membership, drift, heartbeat, period-start lifts) and the outbox queue (write to Jennah). Reconcile is idempotent, so a run that overlaps another is harmless. The function's reserved concurrency for the schedule path is 1.

### D11. The CLI talks to Jennah through the Go SDK
`guardrail pull` reads a version from `config_versions` (newest, or `--version N`) and writes its document to stdout or a file, in the same format `publish` takes. The applied version comes from the newest `config_applied` event in the inbox, so the operator needs read access to the inbox dataset as well; pull prints it with the pending status to stderr, so the file stays clean.

`guardrail setup|pull|validate|diff|publish` uses `jennah-sdk-go` with the operator's own credentials. `publish` reads the applied version, shows the classified diff, then inserts `version = latest + 1` with an idempotency key derived from the document hash, so a retried publish writes one version and a concurrent one fails on the primary key.

## Risks / Trade-offs

- [Budgets lags spend by hours, so a scope can overshoot its ceiling] → The threshold defaults to 100% but the docs recommend 90% for enforce. Phase 2b (pricing create events) is the fix if overshoot is too large.
- [AWS services acting through service-linked roles (Auto Scaling, EKS node groups) are not bound by SCPs, so a frozen account can still scale out] → Documented as a known gap. Opt-in suspend of scale-out is a later change.
- [Usage of existing resources keeps billing during a freeze] → Documented. The freeze stops new resources, not spend.
- [SCP limit of 5 per target, and policy size limits] → The guard uses at most one freeze policy per target. Style B is kept short. Install checks how many SCPs each target already has and reports targets with no free slot.
- [A Jennah credential leak] → Neither credential can write config or decide an approval. The worst a writer can do is add fake events.
- [The approval requester is the guard, so the person who published a loosening can also approve it] → Use `LooseningRequiredApprovals` of 2 for MSPs that need separation. A proper "not the publisher" rule needs the publisher's verified identity on the version, which Jennah doesn't record per row today.
- [Budgets measures AWS cost, not the price a distributor bills its customers] → Agreed with each distributor before any scope enforces. Out of scope here.

## Migration Plan

There is nothing to migrate. Rollout per payer org:

1. The distributor runs `guardrail setup` for the MSP and mints the two credentials.
2. They publish a first config with every scope in dry-run.
3. They deploy the stack through their pipeline and store the credentials in Secrets Manager.
4. After a dry-run month whose `would_freeze` events match what they expect, one MSP moves selected scopes to enforce. That is a tightening change, so it applies at once.

Rollback: delete the stack. Owned budgets are removed by a custom resource on delete, and any attached freeze policy is detached first. Jennah data stays for the record.

## Open Questions

- The exact cost-creating action list for style A and the service list for style B. This changes policy content, not the specs or the tasks.
- Whether Budgets refresh cadence makes a 5-minute config pull pointless. It doesn't change the design, since the pull also serves approvals.
