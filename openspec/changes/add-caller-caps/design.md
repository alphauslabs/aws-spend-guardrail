# Design

## Context

See proposal.md for why. The relevant state today:

- Each trail record keeps `userIdentity.arn` as `Record.CallerARN`; `Usage.Caller` carries it, and hourly `usage_summary` events break usage down by caller. `callerMatches` in `internal/packs/ai/ai.go` already normalizes `arn:aws:sts::<acct>:assumed-role/<role>/<session>` to the role ARN for `ai.api_key_callers`.
- Totals are per account only: `usage#<pack>#<account>` with `m#`, `d#`, and `M#` sort keys (`internal/awsx/activity.go`). Cutoffs are one record per account under `cutoff#<pack>`, and their id is `ai:<account>:<UTC minute>`.
- The engine (`internal/activity/engine.go`) cuts off by attaching the AI deny SCP to the account, and lifts on `ai.lifts`, a raised cap, or a detach seen outside the guard.
- The block policy (`internal/guard/blocked.go`) is the model for a policy whose content the guard writes: one SCP on the root, rewritten when what it denies changes, attached only while it denies something, 5,120-character limit with one alert, and a daily restore of hand edits.
- The trail parser reads `userIdentity.arn` and `type` but not `userIdentity.principalId`, which holds the caller's unique IAM id (`AROA...:<session>` for a role session, `AIDA...` for an IAM user).
- Mantle usage comes from CloudWatch metrics with only account and model (`internal/awsx/mantle.go`).
- `MaxSCPsPerTarget` is 10. The root carries `FullAWSAccess` and, while something is blocked, the block policy.

## Goals / Non-Goals

**Goals:**
- Cap a named role or IAM user per account and deny only that caller, within the fast path's minutes.
- Keep the account caps and tripwires exactly as they are.
- Never let a caller cutoff fail open: if it can't be written, cut off the account.

**Non-Goals:**
- Caps shared by one caller across several accounts.
- Caps per role session or per SSO person (`AWSReservedSSO_<set>/<email>`); a cap on an SSO role covers everyone in that permission set in the account.
- Per-caller tripwires; tripwires still cut off the account.
- Caller attribution for mantle usage, including reconciling from the Cost and Usage Report.
- Wildcards in caller names.

## Decisions

### D1. Callers are named, not given as ARNs

A caller entry is `{"role": "<name>"}` or `{"user": "<name>"}`, with caps. The account comes from the scope (each member account, for an OU scope), so an entry in an OU scope works in every member account without listing ARNs. IAM role and user names are unique within an account regardless of path, so the name alone identifies the caller.

Alternative: full ARNs, as `ai.api_key_callers` takes. Rejected: they tie an OU entry to one account, and role ARNs in the trail drop the path (an assumed-role ARN has no path) while `aws:PrincipalArn` keeps it, so ARNs from the console would often fail to match.

### D2. Normalizing the caller

A shared helper turns `CallerARN` into a caller key:

| Trail `userIdentity.arn` | Key |
|---|---|
| `arn:aws:sts::<acct>:assumed-role/<role>/<session>` | `role/<role>` |
| `arn:aws:iam::<acct>:user/<path/><name>` | `user/<name>` (path dropped) |
| root, `AWSService`, federated user, empty | none: counts toward the account only |

The account in the key is the record's account. `callerMatches` uses the same helper so both stay in step. A short-term Bedrock API key's calls are made by the role session that minted it, so they count toward that role.

`Record` gains `PrincipalID` from `userIdentity.principalId`, and the helper also returns the caller's unique id: the part before `:` for a role session (`AROA...`), the whole value for an IAM user (`AIDA...`). Totals and config stay keyed by name; the unique id is used only to deny (D5).

### D3. Per-caller totals for named callers only

`AddUsage` gains a second write when the usage's caller key is named by the scope covering its account: partition `usage#ai#<account>#<key>` with the same `m#`, `d#`, and `M#` sort keys and the same expiry. The hour-by-model summary rows stay account-only. `Totals` takes an optional caller key.

The fast path passes the engine a `CallersFor(account) map[key]Caps` built with the same account-beats-OU rule as `aiCaps`. Only named callers are counted, so storage writes grow only for those callers, and a caller's totals start when its entry first applies (spec: activity-guard).

Alternative: count every caller always, so a new cap sees today's earlier spend. Rejected: it doubles writes for every priced call in every account to serve callers nobody capped. Adding a caller entry is a tightening the operator controls; the docs say totals start then.

### D4. Caller cutoff records and ids

Caller cutoffs live under `cutoff#ai-caller`, sort key `<account>#<key>`, with `Caller`, `UniqueIDs` (every unique id seen for the caller while cut off), and `Policy` (which caller cutoff policy holds it, 1 to 3, or 0 when it fell back to an account cutoff) fields on `Cutoff`. The id is `ai:<account>:<UTC minute>:<key>`, for example `ai:111122223333:20261120T0930:role/app-chatbot`, so `ai.lifts` takes it without a new field. `LiftIDPattern` widens to accept the optional `:role/<name>` or `:user/<name>` suffix.

The engine evaluates callers after the account. When the account is already cut off (attached), callers in it are skipped. When a call from a cut-off caller carries a unique id the cutoff doesn't hold (the role was deleted and created again), the engine adds the id to the cutoff and its policy, keeping the cutoff id. A caller cutoff names the caller in its event detail (`caller`, `caller_kind`), and uses the existing `cutoff` and `would_cutoff` kinds with event id equal to the cutoff id, so the outbox, the console, and approvals need no new kind. Lifting follows the account rules: a lift in `ai.lifts`, a cap raised or removed, or the caller entry removed. A hand edit to a caller cutoff policy is not a lift: the daily check restores it (D5).

### D5. Caller cutoff SCPs on the root, matched by unique id

The stack creates three placeholder policies, `guardrail-ai-callers-${MSPId}-1` to `-3` (like `AIBlockPolicy`), and the guard writes their content: one `Deny` statement with the AI cutoff policy's action list and

```
"Condition": {"StringLike": {"aws:userid": [
  "AROAEXAMPLEROLEID1234:*",
  "AIDAEXAMPLEUSERID5678"]}}
```

For a role session, `aws:userid` is `<role unique id>:<session name>`, so `AROA...:*` denies every session of that role; for an IAM user it is the user's unique id. A unique id is the same whatever the path, so SSO roles under `/aws-reserved/sso.amazonaws.com/` need nothing special. Each caller costs about 29 characters; with the ~900-character action list, about 140 callers fit in one policy, and about 420 in three. The action list comes from one Go list shared with a test that compares it against the template's `AIDenyPolicy`, so the two can't drift.

A unique id is learned from the trail record of the call that passed the cap, so the guard always has it when it cuts off. A role deleted and created again gets a new id; its totals are kept by name, so its next call is seen as from a cut-off caller and the new id is added (D4).

Each policy is attached to the root while it holds a caller in an enforce scope, and detached otherwise. Attachments, updates, hashes, and hand-edit restores follow `reconcileBlocked` and `checkBlocked`, factored to share the code: one reconciler type, serving the block policy and each caller cutoff policy.

Alternative: match `aws:PrincipalArn` with two ARN patterns per caller (no path, any path). Rejected: 110 to 200 characters per caller, so about 25 to 40 per policy.

Alternative: tag the cut-off role in its member account and deny by `aws:PrincipalTag`. No size limit, but the guard would need a role in every member account, and SSO roles may refuse tags. Left for a later change if an org outgrows 420 callers.

Alternative: attach a per-account policy with the caller condition. Rejected: it uses an SCP slot on each cut-off account and needs the guard to create and delete policies.

Alternative: put callers in the block policy. Rejected: the two change for different reasons and at different speeds (config pull versus fast path), and sharing one size budget would let one starve the other.

### D6. Writing the policies from the fast path, and the fallback

Caller cutoffs are decided in the fast path tick, so the policies are written there, not on the config pull. The engine records the cutoff, then calls `CallerPolicies.Set(cutoffs)`. A new cutoff goes in the lowest-numbered policy with room for it, and stays there until lifted; a lift frees its room, and later cutoffs fill it. Cutoffs never move between policies, so lifting one changes one policy.

When no policy has room, the engine instead cuts off the account with the AI deny policy (an account cutoff with rule `<window>_cap` and detail naming the caller and "caller cutoff policies full"), and alerts once per full stretch. Failing open is not an option: the cap has passed.

When `Set` fails for another reason (API error), the engine alerts once, keeps the caller cutoff recorded but marked not attached, and retries on each tick, like a failed account attach.

### D7. Classification and the stricter merge

`aiChanges` compares caller entries per scope by key: a new entry, a new cap, or a lower cap is a tightening; a removed entry, a removed cap, or a higher cap is a loosening. `stricterAI` keeps, per caller, the lower of each cap, and keeps removed entries until approved. A caller lift id in `ai.lifts` is already a loosening. Approval text: "Raise the daily cap of role app-chatbot in account 111122223333 (name) from $20 to $40."

### D8. Validation

`config/ai.go` checks: exactly one of `role` and `user`; IAM name pattern `^[A-Za-z0-9+=,.@_-]{1,64}$` (so an ARN fails, with a hint to give the name); at least one cap; non-negative caps; no duplicates per scope; and each caller cap no higher than the scope's resolved cap for the same window when that is set. `config/schema.json` gains the field.

### D9. Template revision 9

`TEMPLATE_REVISION` and `MinTemplateRevision` go to 9 with the three new policies, their ids in `AI_CALLER_POLICY_IDS` (comma-separated, in order), and GuardRole permissions: `UpdatePolicy`, `DescribePolicy`, and `ListTargetsForPolicy` on the new policies, and `AttachPolicy`/`DetachPolicy` on them and the root. The lockdown policy in `docs/install.md` gains the same.

## Risks / Trade-offs

- [Mantle spend escapes caller caps] -> It still counts toward the account caps, which stay the backstop. The docs say so, and suggest an account cap whenever caller caps are set.
- [SSO roles cap a whole permission set] -> Documented; per person is a non-goal.
- [Up to three more SCPs on the root] -> With `FullAWSAccess` and the block policy, at most 5 of 10 slots. Only policies holding a caller are attached. The install preflight's root check counts all three as needed.
- [Root SCPs don't reach the management account or service-linked roles] -> Same as account cutoffs; documented. A caller in the management account is counted and reported but not denied, as an account cutoff there is today.
- [A role deleted and created again with the same name] -> Its first calls get through, until the guard sees one and adds the new id, within a tick. Its spend in that tick still counts toward its totals and the account's.
- [A trail record without `principalId`] -> The caller can't be denied by id, so the engine falls back to the account cutoff for that caller, as when the policies are full.
- [Policy writes from the fast path add Organizations API calls] -> Only when the set of caller cutoffs changes, gated by the content hash.

## Migration Plan

1. Release with template revision 9. Operators update template and package together, and update the lockdown policy.
2. No state migration: per-caller keys and `cutoff#ai-caller` are new. Configs without `callers` behave as before.
3. Rollback: removing all caller entries (loosening, approved) lifts every caller cutoff and the guard detaches the policies; then a template downgrade removes it.
