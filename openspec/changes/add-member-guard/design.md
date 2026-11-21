# Design

## Context

See proposal.md for why. What the member guard builds on:

- The guard reads the organization trail's S3 files from the management account (`internal/activity/reader.go`), parses records (`internal/activity/record.go`), prices them (`internal/packs/ai`), keeps totals in DynamoDB (`internal/awsx/activity.go`, partitions `usage#<pack>#<account>` with `m#`, `d#`, `M#` sort keys), and cuts off with SCPs. None of the SCP machinery works from a member account.
- Measured facts (`docs/aws-facts.md`): Bedrock runtime calls are free management events; CloudTrail events reach only the EventBridge bus of the account where the call happened, and only when some trail (an organization trail counts) records them; records carry exact tokens for `Converse`/`InvokeModel` and for streaming through the completion record, the caller, and `callWithBearerToken`; mantle inference carries no tokens or caller in the trail, only per-account CloudWatch metrics.
- The parked `wip/add-caller-caps` (26a8832) has the caller key helper (`role/<name>`, `user/<name>`) and reads `userIdentity.principalId`. Its SCP-based cutoff policies do not carry over.
- The guard's config loop (`internal/guard/pull.go`, about 340 lines) validates a version, splits tightening from loosening (`config.Change`, `config.Direction`), applies the stricter config, raises Jennah approvals with quorum rules, and applies after the delay. It is typed to `config.Resolved`.
- Releases: one workflow on `v*.*.*`, `make release` builds the guard zip and CLIs, `tools/changelogcheck` checks `CHANGELOG.md`, and `internal/releases` reads GitHub's `releases/latest` and accepts only `^vX.Y.Z$`.

## Goals / Non-Goals

**Goals:**
- Cut off a named caller in the client's account within about a minute of the call that passed its cap, and nobody else.
- Install entirely inside the member account, by the client, with no second trail when the organization already has one.
- Reuse the guard's parsing, pricing, shared prices, Jennah client, outbox, and config loop, so there is one implementation of each.
- Release and upgrade independently of the guard.

**Non-Goals:**
- Denying SSO callers. Counted and reported only (D6).
- Account-wide caps, tripwires (unknown model, API key not allowed), and compute packs. The payer guard owns those.
- Caller caps across several accounts, wildcard callers, and mantle attribution.
- The learning agent (per-caller baselines, asking before a cutoff). Phase 2, shared with the guard's agent.
- Protecting the member guard from the account's own admins. The payer guard is the protection the client can't remove.

## Decisions

### D1. Events from the account's own EventBridge buses

Each covered region has an EventBridge rule matching `aws.bedrock` events with detail types `AWS API Call via CloudTrail` and `AWS Service Event via CloudTrail` (the streaming completion record), for the runtime event names the AI pack prices. Rules in other regions forward to the home region's default bus; the home rule sends to one SQS queue; the function reads the queue in batches. Latency is the trail's EventBridge delivery (about 15 to 30 seconds measured in the e2e organization) plus the batch window.

One template serves both roles: a `HomeRegion` parameter, and a condition that creates the queue, function, table, and policies only in the home region, and only the forwarding rule elsewhere. The client deploys it per region, or as a self-managed StackSet across regions.

The daily task calls CloudTrail `LookupEvents` (event history, no trail needed) for `bedrock.amazonaws.com` in each enabled region, to list regions with calls that no rule covers (heartbeat), and to alert when the account has calls in event history but no events arrived (no trail).

Alternatives:
- Its own account trail to S3, read like the guard reads the organization trail. Rejected: when the organization has a trail, a second one is billed as a second copy, and S3 delivery is slower (0.5 to 5 minutes).
- Bedrock model invocation logging with text delivery off. Rejected: it is one account and region setting the client may already use for other purposes, and the guard would have to change it.
- Polling `LookupEvents` as the main source. Rejected: 2 calls per second per region, and minutes of delay.

### D2. One parser for trail files and events

`internal/activity` gains `ParseEvent`, which takes an EventBridge event's `detail` (a CloudTrail record) and returns the same `Record` as `ParseFile`. `Record` gains `PrincipalID` (from `userIdentity.principalId`) and `Session` (the session name of an assumed role). The AI pack's `Evaluate` is unchanged, so pricing, regions, unpriced models, and the data-event double count work as in the guard. Duplicate deliveries are dropped by a conditional put of the CloudTrail `eventID` with a 2-day expiry.

### D3. Caller keys

From the parked helper, extended with sessions:

| `userIdentity` | Key | Session key (when `per_session`) |
|---|---|---|
| `assumed-role/<role>/<session>` | `role/<role>` | `role/<role>/<session>` |
| `user/<path>/<name>` | `user/<name>` | none |
| root, AWS service, federated user | none | none |

A role whose name starts with `AWSReservedSSO_` is an SSO role (D6). The guard's `api_key_callers` matching moves to the same helper.

### D4. Totals and cutoffs in the member account's table

One DynamoDB table per member guard, with the guard's layout: `usage#member#<key>` partitions with `m#`, `d#`, and `M#` sort keys and the same expiry, written only for named callers (named roles, or each session of a `per_session` role). Cutoffs live under `cutoff#member`, keyed by caller key, with the window, cap, total, cutoff id (`member:<account>:<UTC minute>:<key>`), the unique ids seen, and the policy ARN. Reusing `internal/awsx/activity.go` with a pack name of `member` keeps one store implementation.

### D5. Cutoffs as IAM deny policies under the member guard's path

A cutoff creates a customer managed policy under path `/guardrail-member/` and attaches it to the caller's role or user. The policy denies the AI action list (one Go list, tested equal to the guard template's `AIDenyPolicy` actions):

- Role or user cutoff: an unconditional deny.
- Session cutoff: one policy per role holding every cut-off session, with `"StringLike": {"aws:userid": ["AROA...:alice@example.com", ...]}`. The role's unique id comes from `principalId`. A session costs about 45 characters, so about 120 fit in the 6,144-character managed policy limit; beyond that the guard adds a second and third policy for the role, and then denies the whole role and alerts once (fail closed, as the guard does when its caller policies are full).

Role session permissions are evaluated per request, so an attached deny takes effect on sessions already running. The function's IAM permissions are narrow: create, version, and delete policies only under `arn:aws:iam::<account>:policy/guardrail-member/*`, and attach or detach to any role or user only when `iam:PolicyARN` is under that path, with an explicit deny on the stack's own roles (spec: the member guard never cuts off itself).

A 1-minute schedule runs window resets (lifts) and retries failed IAM writes. A 15-minute schedule compares every cutoff with IAM (attached, default version content) and restores drift, alerting once per cutoff.

Alternatives:
- Inline role policies. Rejected: IAM can't scope `PutRolePolicy` to a policy name prefix as tightly as `iam:PolicyARN`, so the function would be able to write any inline policy on any role.
- Deactivating API keys (`UpdateServiceSpecificCredential`). Rejected for v1: it covers keys only, and a policy on the key's user covers both key and console use the same way as other users.
- A permissions boundary. Rejected: it replaces any boundary the client already sets.

### D6. SSO roles are reported, not denied

IAM refuses changes to roles that IAM Identity Center created (expected `UnmodifiableEntity`; confirmed in task 1.1), and their permission sets live in the Identity Center account, which the client's member account can't change. So for a role named `AWSReservedSSO_*`, the member guard records a `would_cutoff` with reason `sso_role` instead of attaching anything, in either mode. Per-person budgets for SSO users would need an SCP from the payer org; that belongs to the payer guard, if ever, and is out of scope.

### D7. Windows reset lifts

Unlike the guard's AI cutoffs, which lift only by approval, a member cutoff lifts at the reset of the window it passed: per-caller budgets are routine limits the client sets for its people, and a developer cut off today should work tomorrow without an approval. At a reset the engine re-checks the caller's other windows and either lifts or re-records the cutoff for the window still passed, keeping one policy attached throughout.

### D8. Config loop shared with the guard

The guard's approval-gated loop is extracted from `internal/guard/pull.go` into `internal/pull` as `Loop[T]`, generic over the resolved config type, with hooks for validate, classify (returning `[]config.Change`), stricter merge, and approval wording. The guard keeps its behavior; its existing tests must pass unchanged, and the extraction ships in a guard release with the guard's full gates. The member guard uses the same loop with `config/member.go`. Stack parameters for approvers, required approvals, and delay are the same as the guard's, with the delay defaulting to 0 for the member guard.

Alternative: a separate, smaller loop for the member guard. Rejected: two implementations of approvals and the stricter merge would drift, against the one-implementation rule.

### D9. Jennah datasets and keys

Dataset ids add the account under the payer org: `guardrail.<msp>.<payer>.<account>.config` and `.inbox` (`schema.MemberConfigDatasetID`, `schema.MemberInboxDatasetID`, `schema.MemberSelector`). So an MSP operator holding `guardrail.<msp>.<payer>.*` reaches the member guards in that payer org, and a client's admins hold only their account's selector. The MSP can tighten a client's caps but can't loosen them, because approvers come from the member stack, which the client controls. The payer guard's keys list exact dataset ids, so they don't reach member datasets.

`guardrail-member setup --msp --payer --account` creates the datasets, declares their tables (layouts versioned in `schema/`, add-only), and prints the reader and writer reach, sharing `internal/cli` with `guardrail setup`.

### D10. Independent releases

- Tags `member-vX.Y.Z`, workflow `.github/workflows/release-member.yml`. The guard's `v*.*.*` filter doesn't match `member-v...`, and the member workflow's `member-v*.*.*` filter doesn't match guard tags.
- `make release-member VERSION=member-vX.Y.Z` builds `member-<tag>-linux-arm64.zip`, the `guardrail-member` CLIs, and the member template into `dist/`, with the same reproducibility flags and `tools/zipdet`.
- Gates: tag on `main`, `tools/changelogcheck -file member/CHANGELOG.md -require Security-relevant`, pinned actions, `govulncheck ./...`, two builds compared, cosign signing. No state fixtures, rollback check, or SBOM.
- Published with `gh release create --latest=false`.
- `internal/releases` lists releases (`/releases?per_page=100`) and picks the newest tag matching a given prefix's `X.Y.Z`: `v` for the guard CLI, `member-v` for the member CLI. This replaces `releases/latest`, so a member release can never be mistaken for a guard release even if marked latest by hand.
- CI runs every package's tests on every push, so a change to shared code is tested for both products before either is tagged.

## Risks / Trade-offs

- [EventBridge events may lack fields the trail file has (tokens in `additionalEventData`, the streaming completion record)] → Task 1.1 confirms on e2e before the parser is built; if a field is missing, the design falls back to an account trail to S3 for that data.
- [IAM changes are eventually consistent] → Task 1.1 measures attach-to-refusal time; the spec allows 2 minutes from call to refusal.
- [SSO users, often the main human Bedrock users, can't be denied] → Reported as would-cutoffs, stated in docs and the heartbeat; the payer guard's account caps remain the backstop.
- [The client's admins can detach policies or delete the stack] → By design (client-managed budgets); drift is restored and recorded, and deleting the stack stops the heartbeat, which the console shows as silent.
- [Uncovered regions] → Listed daily from event history in the heartbeat.
- [Extracting the guard's config loop touches the strict product] → Guard tests unchanged, and the extraction is released through the guard's own gates.
- [Per-session caps on roles whose session names are random] → Every session gets its own small budget, which caps nothing useful; docs say `per_session` suits roles whose session names identify a person or job.

## Migration Plan

1. Land the shared refactors (D2, D3, D8, releases lookup) first, with no behavior change, in a guard release.
2. Add the member guard and its workflow; release `member-v0.1.0`.
3. Remove `openspec/changes/add-caller-caps/`; keep `wip/add-caller-caps` until the caller helper is merged, then delete it.
4. Rollback: a client deletes the member stack; the stack's custom resource detaches and deletes every `/guardrail-member/` policy on delete, so nothing stays denied.
