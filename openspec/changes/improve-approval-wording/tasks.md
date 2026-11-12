# Tasks

## 1. Sentences

- [x] 1.1 Add `Change.Sentence()` in `config/diff.go` with the wording in design D1 for every change kind, including AI fields and a generic fallback; verify with table tests in `config/diff_test.go` that every kind `Diff` produces gets a sentence that passes `alerttext.Check` and contains no field names or arrows
- [x] 1.2 Add the title rule (design D2) as a function in `internal/guard`; test one title per kind and the mixed title

## 2. Approval request

- [x] 2.1 Rewrite `approvalRequest` to use the titles and sentences, add the freeze lines (design D3) and the times (design D4), and add `earliest_apply_at` and `expires_at` to the payload; keep the reason section and the existing payload fields
- [x] 2.2 Tests in `internal/guard`: a raise of a frozen account above spend says it would lift; below spend says it would not; a Cost Explorer error says spend couldn't be read and still raises the approval; an unfrozen scope gets no freeze line; the times match `FirstSeen` plus the delay and window; the summary passes `alerttext.Check`; update `recipients_test.go` for the new title

## 3. Docs and build

- [x] 3.1 Show an example approval in `docs/operating.md` (loosening section) and add a CHANGELOG entry; verify the example matches the test output
- [x] 3.2 Run `gofmt`, `go vet ./...`, and `go test ./...`; verify all pass

## 4. Live check

- [ ] 4.1 With e2e on this build, publish a raise for a frozen scope and confirm the approval email reads as specified: the title, a sentence per change, the freeze line with spend, and the times

## Workflow follow-up

- Deploy the guard to e2e, then archive the change.
