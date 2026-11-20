# Design

## Context

See proposal.md for why. The relevant state today:

- Each trail record keeps `userIdentity.arn` as `Record.CallerARN`; `Usage.Caller` carries it, and hourly `usage_summary` events break usage down by caller. `callerMatches` in `internal/packs/ai/ai.go` already normalizes `arn:aws:sts::<acct>:assumed-role/<role>/<session>` to the role ARN for `ai.api_key_callers`.
- Totals are per account only: `usage#<pack>#<account>` with `m#`, `d#`, and `M#` sort keys (`internal/awsx/activity.go`). Cutoffs are one record per account under `cutoff#<pack>`, and their id is `ai:<account>:<UTC minute>`.
- The engine (`internal/activity/engine.go`) cuts off by attaching the AI deny SCP to the account, and lifts on `ai.lifts`, a raised cap, or a detach seen outside the guard.
- The block policy (`internal/guard/blocked.go`) is the model for a policy whose content the guard writes: one SCP on the root, rewritten when what it denies changes, attached only while it denies something, 5,120-character limit with one alert, and a daily restore of hand edits.
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

### D3. Per-caller totals for named callers only

`AddUsage` gains a second write when the usage's caller key is named by the scope covering its account: partition `usage#ai#<account>#<key>` with the same `m#`, `d#`, and `M#` sort keys and the same expiry. The hour-by-model summary rows stay account-only. `Totals` takes an optional caller key.

The fast path passes the engine a `CallersFor(account) map[key]Caps` built with the same account-beats-OU rule as `aiCaps`. Only named callers are counted, so storage writes grow only for those callers, and a caller's totals start when its entry first applies (spec: activity-guard).

Alternative: count every caller always, so a new cap sees today's earlier spend. Rejected: it doubles writes for every priced call in every account to serve callers nobody capped. Adding a caller entry is a tightening the operator controls; the docs say totals start then.

### D4. Caller cutoff records and ids

Caller cutoffs live under `cutoff#ai-caller`, sort key `<account>#<key>`, with a `Caller` field on `Cutoff`. The id is `ai:<account>:<UTC minute>:<key>`, for example `ai:111122223333:20261120T0930:role/app-chatbot`, so `ai.lifts` takes it without a new field. `LiftIDPattern` widens to accept the optional `:role/<name>` or `:user/<name>` suffix.

The engine evaluates callers after the account. When the account is already cut off (attached), callers in it are skipped. A caller cutoff names the caller in its event detail (`caller`, `caller_kind`), and uses the existing `cutoff` and `would_cutoff` kinds with event id equal to the cutoff id, so the outbox, the console, and approvals need no new kind. Lifting follows the account rules: a lift in `ai.lifts`, a cap raised or removed, or the caller entry removed. A hand edit to the caller cutoff policy is not a lift: the daily check restores it (D5).

### D5. One caller cutoff SCP on the root

The stack creates `guardrail-ai-callers-${MSPId}` as a placeholder (like `AIBlockPolicy`), and the guard writes its content: one `Deny` statement with the AI cutoff policy's action list and

```
"Condition": {"ArnLike": {"aws:PrincipalArn": [
  "arn:aws:iam::111122223333:role/app-chatbot",
  "arn:aws:iam::111122223333:role/*/app-chatbot",
  "arn:aws:iam::111122223333:user/BedrockAPIKey_ab12",
  "arn:aws:iam::111122223333:user/*/BedrockAPIKey_ab12"]}}
```

Two patterns per caller: one for no path, one for any path. Since a name is unique in its account, together they match that caller only. `aws:PrincipalArn` for an assumed-role session is the role ARN, so every session is denied. The action list comes from one Go list shared with a test that compares it against the template's `AIDenyPolicy`, so the two can't drift.

The policy is attached to the root while any enforce caller cutoff exists, and detached otherwise. Attachments, updates, hashes, and hand-edit restores follow `reconcileBlocked` and `checkBlocked`, factored to share the code: one reconciler type, two policies.

Alternative: attach a per-account policy with the caller condition. Rejected: one SCP per cut-off account uses an account's SCP slots and a policy per account, while the root policy uses one slot once.

Alternative: put callers in the block policy. Rejected: the two change for different reasons and at different speeds (config pull versus fast path), and sharing one size budget would let one starve the other.

### D6. Writing the policy from the fast path, and the fallback

Caller cutoffs are decided in the fast path tick, so the policy is written there, not on the config pull. The engine records the cutoff, then calls `CallerPolicy.Set(cutoffs)`. Each caller adds about 110 to 200 characters; with the ~900-character action list, about 25 to 40 callers fit.

When `Set` reports the content would exceed 5,120 characters, the engine instead cuts off the account with the AI deny policy (an account cutoff with rule `<window>_cap` and detail naming the caller and "caller cutoff policy full"), and alerts once per full stretch. Failing open is not an option: the cap has passed.

When `Set` fails for another reason (API error), the engine alerts once, keeps the caller cutoff recorded but marked not attached, and retries on each tick, like a failed account attach.

### D7. Classification and the stricter merge

`aiChanges` compares caller entries per scope by key: a new entry, a new cap, or a lower cap is a tightening; a removed entry, a removed cap, or a higher cap is a loosening. `stricterAI` keeps, per caller, the lower of each cap, and keeps removed entries until approved. A caller lift id in `ai.lifts` is already a loosening. Approval text: "Raise the daily cap of role app-chatbot in account 111122223333 (name) from $20 to $40."

### D8. Validation

`config/ai.go` checks: exactly one of `role` and `user`; IAM name pattern `^[A-Za-z0-9+=,.@_-]{1,64}$` (so an ARN fails, with a hint to give the name); at least one cap; non-negative caps; no duplicates per scope; and each caller cap no higher than the scope's resolved cap for the same window when that is set. `config/schema.json` gains the field.

### D9. Template revision 9

`TEMPLATE_REVISION` and `MinTemplateRevision` go to 9 with the new policy, its id in `AI_CALLER_POLICY_ID`, and GuardRole permissions: `UpdatePolicy`, `DescribePolicy`, and `ListTargetsForPolicy` on the new policy, and `AttachPolicy`/`DetachPolicy` on it and the root. The lockdown policy in `docs/install.md` gains the same.

## Risks / Trade-offs

- [Mantle spend escapes caller caps] -> It still counts toward the account caps, which stay the backstop. The docs say so, and suggest an account cap whenever caller caps are set.
- [SSO roles cap a whole permission set] -> Documented; per person is a non-goal.
- [A third SCP on the root] -> Uses one of 10 slots. The install preflight's root check counts it.
- [Root SCPs don't reach the management account or service-linked roles] -> Same as account cutoffs; documented. A caller in the management account is counted and reported but not denied, as an account cutoff there is today.
- [A role deleted and recreated with the same name] -> The new role inherits the cutoff, since the condition is by ARN. Acceptable: same name, same budget.
- [Policy writes from the fast path add Organizations API calls] -> Only when the set of caller cutoffs changes, gated by the content hash.

## Migration Plan

1. Release with template revision 9. Operators update template and package together, and update the lockdown policy.
2. No state migration: per-caller keys and `cutoff#ai-caller` are new. Configs without `callers` behave as before.
3. Rollback: removing all caller entries (loosening, approved) lifts every caller cutoff and the guard detaches the policy; then a template downgrade removes it.
