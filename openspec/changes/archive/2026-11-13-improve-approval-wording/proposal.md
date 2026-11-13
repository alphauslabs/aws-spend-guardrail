# Proposal

## Why

The loosening approval email is the one place an approver decides whether to weaken protection, and today it reads like a diff: the title says "Loosen spend guard config v8", each change is a raw line such as `account:891376926337: ceiling_usd 5.30 -> 7.00 (loosening)`, the delay prints as Go's `30m0s`, "after it was first seen" doesn't say by whom or when, and nothing tells the approver whether the scope is frozen or what approving would do to the freeze. The user reviewed a real approval on 2026-11-12 and asked for it to be written for a person, as the guard's alerts now are (`fix-alert-wording`).

## What Changes

- The approval title names the kind of change and the payer in plain words, for example "Raise a spend cap for payer 339712710400 (config version 8)", with fixed titles for the few other kinds (removing a scope, changing alert recipients, loosening the AI guard, several kinds at once).
- Each loosening change in the summary is a full sentence, for example "Raise the ceiling of account 891376926337 from 5.30 USD to 7.00 USD."
- For each scope the change touches that is frozen, the summary says so, gives the scope's spend this month from Cost Explorer, and says whether the change would lift the freeze. When spend can't be read, it says the scope is frozen and that its spend couldn't be read.
- The closing paragraph gives the delay in words ("30 minutes"), the earliest time the change can apply (UTC), and when the request expires, and says the stricter config stays in force if it's rejected or unanswered.
- The payload keeps its fields, so automation reading `loosening` is unaffected; it gains `earliest_apply_at` and `expires_at`.
- `guardrail diff` and inbox events keep their current change lines.
- The config-refresh reminder and recovery alerts give the outage length in words ("6 hours", not `6h0m0s`) and the start time in the same UTC form as approvals.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `guard-jennah-contract`: loosening approvals are written for a person, with sentences per change, freeze context, and exact times.

## Impact

- `config/diff.go`: a sentence rendering per change kind, beside `String()`, which stays for `guardrail diff` and events.
- `internal/guard/pull.go` (`approvalRequest`): new title, summary, and payload fields; reads frozen scopes and Cost Explorer spend for frozen scopes the change touches (one $0.01 call per such scope per approval raised).
- Tests in `config` and `internal/guard` that check approval text, including `recipients_test.go`, which checks the old title.
- Docs: `docs/config.md` or `docs/operating.md` where the approval is described; a CHANGELOG entry.
- No change to Jennah, the stack, IAM, or events.
