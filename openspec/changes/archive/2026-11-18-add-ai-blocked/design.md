# Design

## Context

The AI cutoff (`AIDenyPolicy`, `guardrail-ai-cutoff-<msp>`) is a stack-created SCP with fixed content, attached by the guard to one account at a time. AI scopes resolve in `internal/guard/fast.go` (`aiCaps`): each OU scope covers every account in the OU and below, and an account scope beats an OU scope. Config changes are classified in `config/aidiff.go`; `stricterAI` builds what applies at once while a loosening waits.

Facts that shape the design (Service Authorization Reference for `bedrock`, read 2026-11-18):

- Agents, flows, and knowledge bases have their own actions: `bedrock:InvokeAgent`, `InvokeInlineAgent`, `InvokeFlow`, `Retrieve`, `RetrieveAndGenerate` (which also covers `RetrieveAndGenerateStream`), and `AgenticRetrieveStream`.
- `InvokeModelWithBidirectionalStream` has no action of its own: it is authorized as `bedrock:InvokeModel` on the model.
- `StartAsyncInvoke` is authorized as `bedrock:InvokeModel` (with `CallWithBearerToken` and `TagResource`), and `async-invoke` is one of `InvokeModel`'s resource types.
- SCPs don't apply to the management account; one SCP is at most 5,120 characters; a root, OU, or account takes at most 10 SCPs (`docs/aws-facts.md`).

Confirmed in the test organization on 2026-11-18 (task 1.1), with a test SCP holding design D3's four statements, limited to e2enormal by `aws:PrincipalAccount` and attached to the root (967 characters for one account and all four kinds): e2enormal's `InvokeInlineAgent`, `RetrieveAndGenerate`, `Retrieve`, `StartAsyncInvoke` (refused as `bedrock:InvokeModel` on `async-invoke/*`), and Nova 2 Sonic bidirectional stream (refused as `bedrock:InvokeModel` on the model) were each refused "with an explicit deny in a service control policy", while `InvokeModel` on the same embedding model as the async call, and `Converse`, worked. In another member account the same calls got past the SCP and failed on the service's own checks.

## Goals / Non-Goals

**Goals:**
- Let an operator rule out the Bedrock use the guard can't count, per AI scope, with the same tighten-at-once, loosen-with-approval rules as caps.
- Leave everything as it is when `blocked` isn't set.
- Use at most one SCP slot, at the root, and only while something is blocked.

**Non-Goals:**
- Counting or pricing these kinds of use.
- Blocking other things (Rerank, specific models): the model allowlist covers models the guard can see; this list is only for use it can't.
- Blocking in accounts no AI scope covers, or in dry-run scopes.
- Organizations above about 300 blocked accounts (see D3); the scale work is parked.

## Decisions

### D1: `blocked` lives with caps and mode

`AICaps` gains `Blocked []string` (`json:"blocked,omitempty"`), so `ai.defaults` and every scope accept it. Resolution: a scope that sets `blocked` (including `[]`) uses its own list; one that doesn't inherits `ai.defaults.blocked`. `ResolvedAIScope` carries the resolved, sorted list. A nil slice is "unset" and an empty one is "none": JSON decoding keeps them apart (an absent field stays nil, `[]` becomes empty), as the model and region allowlists already rely on.

### D2: Only enforce scopes block

A dry-run scope blocks nothing, as dry-run attaches nothing anywhere else. There is nothing to report as "would block": the guard can't see the use. `guardrail diff` notes when a dry-run scope sets `blocked`, so it isn't mistaken for active.

### D3: One policy at the root, listing accounts by condition

The stack creates `AIBlockPolicy` (`guardrail-ai-blocked-<msp>`, SCP) with placeholder content that allows everything and denies nothing. The guard rewrites its content with `organizations:UpdatePolicy` and attaches it to the organization root while any account has something blocked; it detaches it when none does. CloudFormation updates a resource only when its template properties change, so a stack update doesn't reset the content the guard wrote, unless a release changes the placeholder.

The content has one `Deny` statement per distinct set of blocked kinds, with the actions and resources of those kinds and `Condition: {StringEquals: {aws:PrincipalAccount: [accounts]}}`. Accounts with the same set share a statement, so each blocked account appears once. Per kind:

| Kind | Action | Resource |
|---|---|---|
| `agents` | `bedrock:InvokeAgent`, `bedrock:InvokeInlineAgent`, `bedrock:InvokeFlow` | `*` |
| `knowledge_bases` | `bedrock:Retrieve`, `bedrock:RetrieveAndGenerate`, `bedrock:AgenticRetrieveStream` | `*` |
| `async_invoke` | `bedrock:InvokeModel` | `arn:aws:bedrock:*:*:async-invoke/*` |
| `bidirectional_streams` | `bedrock:InvokeModel`, `bedrock:InvokeModelWithResponseStream` | `arn:aws:bedrock:*::foundation-model/*sonic*`, `arn:aws:bedrock:*:*:inference-profile/*sonic*` |

A set with several kinds whose resources differ needs one statement per resource pattern; statements are generated per (set, resource group) and accounts listed once per statement.

With about 16 characters per account id in a list and a fixed overhead of about 1,000 characters, about 250 to 300 blocked accounts fit. When the content would exceed 5,120 characters, the guard writes nothing, keeps the last policy, and alerts once with how many accounts didn't fit.

*Alternatives:* attaching a policy to each blocked account would use a slot in every account (the cutoff and freezes already use slots); a policy per kind would need up to four slots per account; a policy per OU path (`aws:PrincipalOrgPaths`) would be more compact but breaks on account-scope overrides inside a blocked OU. The single root policy is the smallest footprint and the simplest to keep in step.

### D4: Bidirectional streams are blocked by model

AWS gives a stream no action or resource of its own, so `bidirectional_streams` denies `InvokeModel` on the models that serve streams: Nova Sonic, by an id pattern `*sonic*`. This also denies any other use of those models, which today is none in practice (they're speech-to-speech models). Docs say so, and the pattern is pinned by a test, to revisit when AWS adds a stream-capable model of another family.

### D5: Reconciled on every pull

On each pull, once the config in force is known, the guard computes each covered enforce account's blocked set (with `aiCaps`'s account-beats-OU rule), builds the content, and compares it with what it last wrote (kept in state with a hash). If different, it updates the policy, then attaches or detaches as needed, then records the new hash and emits `ai_blocked` with the accounts and kinds that changed. The daily drift check reads the policy's content and attachment, and rewrites them if someone changed them by hand, alerting once.

Failures (access denied, root SCP limit reached, policy too large) alert once per failing stretch, keep retrying on each pull, and alert on recovery, like the shared-prices status.

### D6: Classification

`aiChanges` compares each account target's resolved blocked set: each kind added is `Tightening`, each kind removed `Loosening`, on the field `ai.blocked`. `stricterAI` takes the union of applied and desired sets per scope, so additions apply at once and removals wait. The approval sentence reads: "Allow agents on account 730335604511 (e2enormal); agent use isn't counted toward its caps." A scope added in enforce mode with `blocked` is a tightening, as adding a scope is today; a scope removed is already a loosening.

### D7: Permissions and template

The guard role gains, in the AI guard policy: `organizations:UpdatePolicy` and `organizations:DescribePolicy` on `AIBlockPolicy`, `organizations:AttachPolicy` and `DetachPolicy` on `AIBlockPolicy` and the root (`arn:aws:organizations::<payer>:root/o-*/r-*`), and `organizations:ListRoots` and `organizations:ListTargetsForPolicy`. Template revision 6.

## Risks / Trade-offs

- **A mistake at the root affects every member account.** Every deny statement carries an account condition, built from a non-empty list; a test asserts no statement is generated without one, and that the placeholder denies nothing.
- **AWS refuses to delete an attached policy.** On stack deletion, the existing teardown step, which runs before the AI policies are deleted, also detaches the block policy, so deleting the stack needs nothing first.
- **The root's 10-SCP limit.** If the root is full, attaching fails; the guard alerts once and blocks nothing until a slot is free. Preflight reports the root's free slots.
- **Async invoke denial depends on `async-invoke` resource evaluation.** Confirmed by task 1.1 (below).
- **Rollback** to v0.2.6: its template has no `AIBlockPolicy`, so the update tries to delete it, which AWS refuses while it is attached; the policy would stay attached with no guard to update it. The upgrade notes say to clear `blocked` (approved, so the guard detaches the policy) before rolling back.
