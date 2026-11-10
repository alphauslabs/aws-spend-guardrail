# Proposal

## Why

Installing the guard in the AWS console means filling a CloudFormation form with about a dozen parameters: the MSP id, two secret ARNs, approver emails, a bucket and key. Whoever runs the payer account may not use the AWS CLI, and a typo in that form produces a guard that reads the wrong config or can't start. A CloudFormation quick-create link opens the console with the template and every known value already filled in, so the person only checks the form and confirms.

## What Changes

- **A `guardrail install-link` command.** Given the MSP id, payer account, the S3 bucket and prefix where the release's `template.yaml` and `guard.zip` were uploaded, and optionally the secret ARNs, approvers, and alert email, it prints a quick-create URL for the CloudFormation console in us-east-1. Values not given are left for the form.
- **Offline and secret-free.** The command needs no AWS or Jennah credentials and makes no network calls. The link carries only identifiers and ARNs, never a key; a value that looks like a Jennah key is refused.
- **A wrong-account guard in the template.** A new optional parameter, `ExpectedPayerAccountId`, makes stack creation fail when the stack is created in any other account. The link always sets it, so a link for one payer org can't install the guard in another.
- **The console guide uses the link.** `docs/install-console.md` gets a short path: upload the two release files, open the link, check the form, and create the stack.

Non-goals: hosting the template or package in an Alphaus-owned bucket (the release design keeps the package in the client's own bucket so they deploy what they verified), uploading files for the user, a link for StackSets, and creating the secrets or the Jennah keys from the link.

## Capabilities

### New Capabilities

- `guard-install`: how a payer org gets its guard stack: the quick-create link, what it may and may not contain, and the template's refusal to install in the wrong account.

### Modified Capabilities

None. The template parameter is additive, and the existing install steps keep working.

## Impact

- **CLI:** a new `install-link` subcommand in `cmd/guardrail`, with its URL building in `internal/cli`.
- **Template:** one new optional parameter, `ExpectedPayerAccountId`, and a `Rules` assertion on it. Existing stacks are unaffected while it stays empty.
- **Docs:** `docs/install-console.md` and `docs/operating.md`.
- **Related:** `add-verifiable-releases` publishes `template.yaml` and `guard.zip`; the link points at the client's copies of them, consistent with that change's rule that the template references no Alphaus bucket.
