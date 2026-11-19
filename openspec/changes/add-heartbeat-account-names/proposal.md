# Proposal

## Why

The guard's alerts name accounts as `<id> (<name>)` (add-account-names-in-alerts), but its heartbeat carries only ids. A reader of the heartbeat (the Jennah console's payer page) shows bare ids for frozen scopes, excluded and unprotected accounts, AI cutoffs, blocks, and accounts not linked for mantle, so an operator reading the page has to look up names the email already gave them.

## What Changes

- Each heartbeat gains `account_names`: id to name, for the accounts its state mentions (account scopes, excluded and unprotected accounts, and the AI section's cutoffs, blocks, and unlinked accounts). Accounts with no known name are left out; when the organization can't be listed, the field is left out.
- Names are not part of the digest: a rename alone doesn't write a heartbeat, and the next daily or change heartbeat carries the new name.
- `state_layout` stays 1: the field is an addition.
- In the Jennah console, the payer page shows `<id> (<name>)` wherever its cards show an account, as the alerts do. The events table stays as it is.

## Capabilities

### New Capabilities

### Modified Capabilities

- `guard-jennah-contract`: the heartbeat names the accounts its state mentions.

## Impact

- aws-spend-guardrail: `internal/guard/heartbeat.go` (names added beside spend, after the digest, from the guard's existing `Names` hook and its hourly cache), tests, `docs/operating.md` ("The guard's state"), CHANGELOG. No stack change: the guard already lists accounts.
- jennah: `app/src/lib/guardrail.ts` (reading `account_names`), the payer page, tests; an `app-v*` release.
