# Tasks

## 1. Engine and guard

- [ ] 1.1 `activity.Policies.Attached`, implemented in `awsx.PackPolicies` with `ListTargetsForPolicy` (paged), and in the test fakes
- [ ] 1.2 In `lifts`, while any attached cutoff is held, read the policy's accounts once and treat a cutoff older than 5 minutes whose account is missing as lifted, detached outside the guard: delete the record, emit `cutoff_lifted` and `drift`, alert once (design D1, D2); skip on a failed read
- [ ] 1.3 Daily: report, once per account per day, the AI cutoff policy attached without a recorded cutoff, with `drift` and an alert, without detaching (D3)
- [ ] 1.4 Tests: a hand-detached cutoff is forgotten with one alert and events, and a new breach cuts the account off again with a new id; a cutoff just attached isn't taken as detached; a failed read forgets nothing; nothing is read with no cutoffs held; an unrecorded attachment is reported once a day and left attached

## 2. Stack and docs

- [ ] 2.1 Template: `organizations:ListTargetsForPolicy` in the `AICutoff` statement, `TEMPLATE_REVISION` and `MinTemplateRevision` 7; template tests and cfn-lint pass
- [ ] 2.2 Docs: the operating guide's emergency section and `ai-guard.md` say the guard notices a detached cutoff within minutes and watches the account again; CHANGELOG with Upgrade (stack update, revision 7), Fixed, and Security-relevant (the new read permission); docs style (no em-dashes, Oxford comma)
- [ ] 2.3 Run `goimports`, `make`, `go vet ./...`, `go test ./...`, and cfn-lint; verify all pass

## 3. Live check

- [ ] 3.1 On e2e with the AI guard on and a test trail, cut e2enormal off (an unpriced model in an enforce scope), detach the cutoff policy by hand, and confirm within a few minutes the `cutoff_lifted` event with the reason, one alert, and that a new unpriced call cuts it off again; then lift through config and remove the test setup
