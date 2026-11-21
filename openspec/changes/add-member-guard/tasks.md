# Tasks

## 1. Facts on e2e

- [ ] 1.1 In an e2e member account (organization trail on), confirm and record in `docs/aws-facts.md`: EventBridge `AWS API Call via CloudTrail` events for `Converse` and `InvokeModel` carry `additionalEventData` token counts and `userIdentity.principalId`; streaming calls deliver their completion record as `AWS Service Event via CloudTrail` with tokens; a cross-region rule forwarding to another region's bus adds how much latency; IAM refuses attaching a policy to an `AWSReservedSSO_*` role (error code); a deny attached to a role refuses calls from an already running session within how many seconds; a `StringLike aws:userid` `AROA...:<session>` deny refuses only that session name. Verify each row has status and evidence, and remove the temporary roles, rules, and policies.

## 2. Shared refactors (no behavior change)

- [ ] 2.1 Add `Record.PrincipalID` and `Record.Session` and `activity.ParseEvent` for an EventBridge detail (D2), salvaging `principalId` parsing from `wip/add-caller-caps`; verify record tests for a role session, an IAM user, an API key call, a streaming completion, and an EventBridge event giving the same `Record` as the same record in a trail file.
- [ ] 2.2 Move the caller key helper into `internal/packs/ai` with session keys and SSO detection (D3), and use it in `callerMatches`; verify tests for an assumed role, a session key, a user with a path, an API key user, an SSO role, root, and an AWS service, and that the existing `api_key_callers` tests pass unchanged.
- [ ] 2.3 Extract the config loop from `internal/guard/pull.go` into `internal/pull` as `Loop[T]` (D8), with the guard as its first user; verify every existing `internal/guard` test passes unchanged and `go vet ./...` is clean.
- [ ] 2.4 Change `internal/releases` to list releases and pick the newest tag for a prefix (`v` or `member-v`) (D10), and use prefix `v` in `guardrail versions`; verify tests where the newest release is `member-v0.1.0` and the guard comparison still uses `v0.3.0`, and where GitHub is unreachable.
- [ ] 2.5 Add a CHANGELOG entry for the guard release that carries 2.1 to 2.4 (Upgrade: None; Security-relevant: none), and verify `go run ./tools/changelogcheck CHANGELOG.md <tag>`.

## 3. Member config

- [ ] 3.1 Add `config/member.go` and `config/member.schema.json` (mode, callers with role or user, `per_session`, caps, lifts) and validation per the `member-config` spec; verify a table test covering each refusal and the field it names, and that the docs example validates.
- [ ] 3.2 Classify member changes into `[]config.Change` (tightening and loosening per the spec) with the stricter merge and approval sentences; verify tests: a new caller and a lower cap tighten, a raised cap, removed entry, `per_session` off, enforce to dry-run, and a lift loosen, and a mixed version applies its tightening at once.
- [ ] 3.3 Refuse a config naming the member guard's own function role (from the environment); verify with a test.

## 4. Member Jennah contract

- [ ] 4.1 Add member dataset ids, selector, and table layouts to `schema/` (D9); verify tests that member ids sit under the payer selector and that a client's selector reaches only its account.
- [ ] 4.2 Add `cmd/guardrail-member` with `setup --msp --payer --account` (datasets, tables, printed reader and writer reach including `guardrail.prices`), `validate`, `publish`, and `versions`, sharing `internal/cli`; verify CLI tests against the jennahtest fake, including that the printed reader reach doesn't include any payer dataset.
- [ ] 4.3 Write member events (cutoff, would-cutoff, lift, restored, config applied or rejected, approvals, heartbeat) through the outbox with the release version and code hash; verify event tests for each kind and that an event written twice is delivered once.

## 5. Counting and cutoffs

- [ ] 5.1 Add `internal/member` engine: take EventBridge records from SQS, drop duplicates by event id, price with the AI pack and shared prices, and write totals for named callers and `per_session` sessions only (D4); verify engine tests for a named role, an unnamed role (nothing stored), a session of a `per_session` role, a mantle record (not counted), and a duplicate event.
- [ ] 5.2 Add the IAM deny writer (D5): create, update, attach, detach, and delete policies under `/guardrail-member/`, the shared AI action list, session policies with `aws:userid`, and overflow into second and third policies and then the whole role; verify tests for each content shape, that 120 sessions fit in 6,144 characters, and that the action list equals the guard template's `AIDenyPolicy` actions.
- [ ] 5.3 Cut off on a passed cap in enforce, record would-cutoff in dry-run (once per caller and window), and record would-cutoff with reason `sso_role` for SSO roles (D6); verify engine tests for each.
- [ ] 5.4 Lift at window resets with the re-check of other windows (D7), and on approved raises, removals, and lifts; verify engine tests for a daily reset, a reset while over the monthly cap, and a raised cap.
- [ ] 5.5 Restore drift every 15 minutes (detached, edited, or deleted policy) with one alert per cutoff; verify tests for each kind of drift.
- [ ] 5.6 Daily task: heartbeat with entries, cutoffs, covered regions, uncovered regions from `LookupEvents`, and SSO would-cutoffs, plus the no-trail alert (D1); verify tests for an uncovered region and an account with no events.

## 6. Member stack

- [ ] 6.1 Add `member/template.yaml` (D1, D5): `HomeRegion` condition, forwarding rules, home rule, SQS queue, function from a client-chosen S3 package, table, alert topic, Jennah key secrets, approver parameters, 1-minute and 15-minute schedules, the narrow IAM permissions with the explicit deny on its own roles, and a delete-time custom resource that removes every `/guardrail-member/` policy; verify cfn-lint in CI and a template test of the IAM statements.
- [ ] 6.2 Add `member/main.go` wiring the engine, the config loop, the outbox, and the schedules, reporting its version and code hash; verify `member/main_test.go` and `make build`.
- [ ] 6.3 Write `docs/member-guard.md`: what it does and doesn't (SSO, mantle, the client's admins, uncovered regions), install per region or by StackSet, the trail requirement, config reference, lifts, and `per_session` guidance; verify the doc's config example validates with `guardrail-member validate`.

## 7. Member releases

- [ ] 7.1 Add `make release-member` (zip, CLIs, template, sums) and `.github/workflows/release-member.yml` on `member-v*.*.*` with the light gates and `--latest=false` (D10); verify the workflow passes `tools/pincheck`, and `make release-member VERSION=member-v0.0.0` run twice gives identical `SHA256SUMS`.
- [ ] 7.2 Teach `tools/changelogcheck` a changelog path and required sections, and add `member/CHANGELOG.md` with a `member-v0.1.0` entry; verify the check passes for it and fails for an entry without a Security-relevant section.
- [ ] 7.3 Add a member section to `docs/verify.md` (signer identity of the member workflow, member asset names); verify the commands against a dry-run build.
- [ ] 7.4 Lint `member/template.yaml` in `.github/workflows/ci.yml`; verify CI passes on a branch push.

## 8. Integration

- [ ] 8.1 On an e2e member account with the organization trail: install the member stack in us-east-1 with a forwarder in us-west-2, cap a test role at a small daily amount and a `per_session` role per session, call Bedrock until each passes, and verify only that role or session is refused while another role works, that an SSO role yields a would-cutoff, that a detached policy is restored, and that deleting the stack leaves no `/guardrail-member/` policy. Remove the test roles and stacks afterwards.

## 9. Cleanup

- [ ] 9.1 Remove `openspec/changes/add-caller-caps/`, and verify `openspec list` no longer shows it.

## Workflow follow-up

- Jennah console: show member guards and caller cutoffs on the guardrail pages, and make the console's latest-release check ignore `member-v*` tags (jennah repo, its own change).
- Release the guard with the shared refactors, then `member-v0.1.0`; deploy to e2e; archive the change.
- Delete `wip/add-caller-caps` once 2.1 and 2.2 are merged.
