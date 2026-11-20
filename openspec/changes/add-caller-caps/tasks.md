# Tasks

## 1. Config

- [ ] 1.1 Add `callers` to `AIScope` and `ResolvedAIScope` in `config/ai.go` (entries `{role|user, hourly_usd, daily_usd, monthly_usd}`, sorted by key, no defaults inherited) and to `config/schema.json`; verify with resolve tests for an account and an OU scope.
- [ ] 1.2 Validate caller entries (D8): one of role or user, IAM name pattern with an ARN hint, at least one cap, non-negative, no duplicates, caller cap not above the scope cap; verify with a table test covering each refusal and the field it names.
- [ ] 1.3 Widen `LiftIDPattern` to accept the `:role/<name>` and `:user/<name>` suffix; verify a caller lift id validates and a malformed one fails.
- [ ] 1.4 Classify caller changes in `aiChanges` and merge them in `stricterAI` (D7); verify with tests: a new entry and a lower cap tighten, a raised cap, removed cap, and removed entry loosen and stay out of the stricter config until approved.
- [ ] 1.5 Word caller changes in approval text (`internal/guard/approvaltext.go`), naming the caller, the account with its name, and both caps; verify with an approval text test.
- [ ] 1.6 Document `callers` in `docs/ai-guard.md` (config, how callers are matched, SSO roles, API keys, totals starting when the entry applies, mantle not counted, management account not denied) and in the config reference; verify the doc's example validates with `guardrail validate`.

## 2. Counting

- [ ] 2.1 Read `userIdentity.principalId` into `Record.PrincipalID` in `internal/activity/record.go`; verify with record tests for a role session, an IAM user, and a record without it.
- [ ] 2.2 Add the shared caller helper in `internal/packs/ai` (D2), returning the caller key and unique id, and use it in `callerMatches`; verify with tests for an assumed role, a user with a path, an API key user, root, and an AWS service.
- [ ] 2.3 Add caller totals to the store (D3): `AddUsage` writes `usage#ai#<account>#<key>` when the caller is named, `Totals` takes a caller key; verify with `internal/awsx` tests and the activitytest fake.
- [ ] 2.4 Pass `CallersFor` from `internal/guard/fast.go` with the account-beats-OU rule; verify an OU entry gives each member account its own budget in a fast-path test.
- [ ] 2.5 Confirm mantle usage writes no caller totals; verify with a metrics-reader test where a role's mantle-only spend passes its cap and nothing is cut off.

## 3. Caller cutoffs

- [ ] 3.1 Add caller cutoff records under `cutoff#ai-caller` with the `Caller`, `UniqueIDs`, and `Policy` fields and the `ai:<account>:<minute>:<key>` id (D4); verify store round-trip tests.
- [ ] 3.2 Evaluate caller caps in the engine after the account, skipping cut-off accounts, emitting `cutoff` or `would_cutoff` with `caller` and `caller_kind`, and alerting; verify engine tests for enforce, dry-run (once per hour), and an account already cut off.
- [ ] 3.3 Lift caller cutoffs on `ai.lifts`, a raised or removed cap, or a removed entry; verify engine tests for each, and that a later breach gets a new id.
- [ ] 3.4 Add a new unique id seen for a cut-off caller to its cutoff and policy, keeping the cutoff id; verify with an engine test of a role created again.
- [ ] 3.5 Factor the block policy reconciler so it serves the block policy and each caller cutoff policy, and build caller cutoff content with `StringLike aws:userid` (D5) from a shared action list; verify `blocked_test.go` still passes, a content test for role and user ids, a test that 140 callers fit in 5,120 characters, and a test that the shared list equals the template's `AIDenyPolicy` actions.
- [ ] 3.6 Write the caller cutoff policies from the fast path, filling the lowest-numbered policy with room and never moving a cutoff, attaching each to the root while it holds an enforce caller and detaching it otherwise; when all are full or the unique id is unknown, cut off the account instead and alert once, and on an API error alert once and retry (D6); verify engine tests for each path, including a spill into the second policy.
- [ ] 3.7 Restore hand edits to each caller cutoff policy in the daily check, alerting once; verify with a test like `checkBlocked`'s.
- [ ] 3.8 Document caller cutoffs, lifts, the unique-id matching (and a role created again), the three policies and the overflow fallback, and limits in `docs/ai-guard.md` and `docs/operating.md`; verify the lift example id matches `LiftIDPattern`.

## 4. Stack

- [ ] 4.1 Add `AICallerPolicy1` to `AICallerPolicy3` to `guard/template.yaml` with the GuardRole permissions, `AI_CALLER_POLICY_IDS`, and `TEMPLATE_REVISION: "9"`; set `MinTemplateRevision = 9`; verify cfn-lint, `guard/main_test.go`, and the deployment tests.
- [ ] 4.2 Count the three caller cutoff policies in the install preflight's root slot check; verify with preflight tests at 7 and 8 root SCPs.
- [ ] 4.3 Update `docs/install.md` (resource list, lockdown policy) and add a CHANGELOG entry with Upgrade and Security-relevant sections; verify `go run ./tools/changelogcheck CHANGELOG.md vX`.

## 5. Integration

- [ ] 5.1 On the e2e payer, with the AI guard on and a temporary trail: cap a test role at a small daily amount in an enforce scope, call Bedrock until it passes, and verify the role is refused while another role in the account still works, then lift through `ai.lifts` with approval and verify the role works again and the policy is detached. Remove the test roles, trail, and bucket afterwards.

## Workflow follow-up

- Jennah console: show the caller in cutoff and would-cutoff event summaries (`app/src/lib/guardrail.ts`), as a small console change.
- Release, deploy to e2e, and archive the change.
