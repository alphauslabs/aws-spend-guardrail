# Tasks

## 1. Template

- [ ] 1.1 Add the `ExpectedPayerAccountId` parameter and the `RightAccount` rule (design D3); verify `cfn-lint` passes, and that a stack created with a different account id fails before creating any resource while an empty value deploys as before (validated in the e2e organization)

## 2. CLI

- [ ] 2.1 Implement the link builder in `internal/cli` (design D1, D2): input checks for MSP id, payer, bucket name, secret ARNs (Secrets Manager, us-east-1, payer account), and approver emails; refusal of any `jennah_sk_` value (design D4); URL encoding; verify table tests for each refusal, the encoded URL for a full and a minimal set of options, and a length under 2,000 characters
- [ ] 2.2 Add `guardrail install-link` to `cmd/guardrail` with its flags and help text; verify it runs with no network and no credentials, and prints only the URL on stdout

## 3. Docs

- [ ] 3.1 Add a "with a link" path to `docs/install-console.md` (upload the two release files, open the link, check, create) and the command to `docs/operating.md`; verify the documented command prints a working link

## 4. Integration

- [ ] 4.1 In the e2e organization, generate a link, open it in the console signed in to the payer account, and create the stack; then open the same link signed in to another account and confirm it is refused

## Workflow follow-up

- Archive after 4.1.
