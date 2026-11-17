# Tasks

## 1. Confirm the hole

- [x] 1.1 On e2e with the AI guard on and e2enormal (730335604511) cut off by the current policy (by the guard in enforce mode, or by attaching `guardrail-ai-cutoff-e2e` by hand for the test), call the `bedrock-mantle` endpoint's `/v1/chat/completions` from e2enormal and confirm it succeeds, and that `bedrock-runtime` `Converse` is refused; record the result in the design

## 2. Policy

- [x] 2.1 Extend `AIDenyPolicy` to the actions in design D1, and update its comment
- [x] 2.2 Add the template test pinning the denied actions and checking the policy's JSON is under 5,120 characters (design D4)
- [x] 2.3 Bump `TEMPLATE_REVISION` and `MinTemplateRevision` to 3 (design D3); the existing match test passes

## 3. Docs and build

- [x] 3.1 Update `docs/ai-guard.md` (What a cutoff blocks: the full list grouped by kind, what stays allowed, that running work isn't stopped); add CHANGELOG entries with an Upgrade section (update the stack with the new template; accounts already cut off get the wider cutoff at once; rollback supported) and Security-relevant (the policy's new actions); follow the docs' style (no em-dashes, Oxford comma)
- [x] 3.2 Run `goimports`, `make`, `go vet ./...`, `go test ./...`, and cfn-lint; verify all pass

## 4. Live check

- [x] 4.1 With e2enormal still cut off, update the stack to this build and confirm, without a new cutoff, that the mantle call from 1.1 is now refused, `Converse` is still refused, and a read (`ListFoundationModels`) and an S3 call still work; then lift the cutoff and remove the test setup
