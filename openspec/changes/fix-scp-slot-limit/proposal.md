# Proposal

## Why

`guardrail preflight` reports an account or OU as having no free SCP slot when it already has 5 SCPs attached directly, and the install docs tell people to detach or merge a policy at 5. AWS Organizations now allows 10 SCPs attached directly to a root, OU, or account, and a policy document of up to 10,240 characters. So the check refuses targets that have room for 5 more policies, and someone following the docs would detach policies for nothing.

## What Changes

- **The slot check uses AWS's current limit of 10** SCPs attached directly to one target. A target is reported as full at 10, not 5.
- **The docs say 10,** in the operating guide, the console install steps, and the record of AWS facts, which also records the 10,240-character size limit and the measured time for an attached SCP to take effect.

Non-goals: reading the limit from AWS at run time (it is a hard limit that Service Quotas doesn't expose per target), and any change to how freezes are attached.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `spend-freeze`: states the limit the install check applies. Introduced by `add-budget-freeze-guard`, not archived yet, so the delta adds a requirement to it.

## Impact

- **Code:** `internal/awsx/preflight.go` (`MaxSCPsPerTarget`) and its test. The `preflight` command's output prints the limit from the same constant.
- **Docs:** `docs/operating.md`, `docs/install-console.md`, `docs/aws-facts.md`, and `CHANGELOG.md`.
- **Deployed guards:** unaffected. Only the operator's `preflight` command and the docs change.
