# Tasks

## 1. Facts first

- [x] 1.1 In e2e, by hand: attach a test SCP to the organization root that denies, for e2enormal only (`aws:PrincipalAccount`), the four kinds as design D3's table lists; confirm from e2enormal that `InvokeInlineAgent` and `RetrieveAndGenerate` are refused by the SCP (not only by maintenance mode or validation), `StartAsyncInvoke` is refused while `InvokeModel` on the same model works, a Nova Sonic bidirectional stream is refused, and `Converse` works; confirm another member account is unaffected; record the results in the design and `docs/aws-facts.md`, then detach and delete the test SCP

## 2. Config

- [x] 2.1 `config/ai.go`: `Blocked` on `AICaps` and the resolved list on `ResolvedAIScope` (design D1); validation of values and duplicates; `schema.json`
- [x] 2.2 `config/aidiff.go`: classify added kinds as tightening and removed ones as loosening per target, and take the union in `stricterAI` (D6); approval sentences in `sentence.go`
- [x] 2.3 `guardrail diff` notes a dry-run scope that sets `blocked` (D2)
- [x] 2.4 Tests: inheritance and override, `[]` blocks nothing, unknown and duplicate values refused, add applies at once, remove waits with the sentence naming the account, stored state from v0.2.6 (no `blocked`) loads as nothing blocked

## 3. Guard

- [x] 3.1 Per-account blocked sets from enforce AI scopes with the account-beats-OU rule; the policy content per design D3 with one statement per (set, resource group), accounts listed once each, and a size check
- [x] 3.2 Reconcile on each pull (D5): update the policy when its content changed, attach to the root when something is blocked and detach when nothing is, record a hash in state, emit `ai_blocked` with what changed; alert once per failing stretch (too large, access denied, root full) and on recovery
- [x] 3.3 Daily: compare the policy's live content and attachment with what the guard wrote, and rewrite them, alerting once, when they differ
- [x] 3.4 `internal/awsx`: `UpdatePolicy`, `DescribePolicy`, the root id (`ListRoots`), and attachment checks
- [x] 3.5 Tests: content for mixed sets; no deny statement without an account condition; the placeholder denies nothing; too large keeps the last policy and alerts once; nothing blocked detaches; a failed attach alerts once and recovers; hand edits are rewritten by the daily check

## 4. Stack and docs

- [x] 4.1 Template: `AIBlockPolicy` (placeholder content, AI guard only), the role's permissions (D7), `TEMPLATE_REVISION` and `MinTemplateRevision` 6; template tests and cfn-lint pass
- [x] 4.2 `guardrail preflight --ai` reports the root's free SCP slots when any scope sets `blocked`
- [x] 4.3 Docs: `config.md` (the field, inheritance, enforce only, what each kind denies), `ai-guard.md` (blocking what the guard can't count, the root policy, limits), `aws-facts.md` (the 1.1 results), uninstall and rollback notes (clear `blocked` first); CHANGELOG with Upgrade, Security-relevant (a new SCP the guard writes and attaches at the root, new Organizations permissions), and rollback; docs style (no em-dashes, Oxford comma)
- [x] 4.4 Run `goimports`, `make`, `go vet ./...`, `go test ./...`, and cfn-lint; verify all pass

## 5. Live check

- [x] 5.1 On e2e with the AI guard on and e2enormal's AI scope in enforce mode with `blocked: ["agents", "async_invoke"]`: within 5 minutes the policy is attached at the root and lists e2enormal; e2enormal's `InvokeInlineAgent` and `StartAsyncInvoke` are refused and `Converse` works; another member account is unaffected
- [x] 5.2 Publish a version allowing `agents` for e2enormal: it waits for approval with the expected sentence; after approval and the delay, agent calls are no longer refused by the policy, and async invokes still are
- [x] 5.3 Publish a version blocking nothing, approve it, and confirm the policy is detached from the root; then restore the e2e config and stack (AI guard off) and remove any test setup
