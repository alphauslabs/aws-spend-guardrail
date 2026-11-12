## Why

The guard has never been able to raise a loosening approval against a real Jennah. Its approval request leaves the quorum rule unset, and Jennah rejects that ("A quorum rule is required: ANY_ONE or ALL."). The e2e lift test found this: raising a frozen account's ceiling failed, the stricter config stayed in force, and the stack alerted. Unit tests missed it because the test fake accepts any request. Until this is fixed, no loosening applies: no raised ceiling, no lifted freeze, no removed scope, and no alert-recipient change.

Separately, the stack and docs suggest `LooseningRequiredApprovals=2` for separation of duties. Jennah only supports "any one approver" or "all approvers", so 2 works only when there are exactly two approvers.

## What Changes

- The guard's approval request carries a quorum rule: `ANY_ONE` when `LooseningRequiredApprovals` is 1, and `ALL` when it equals the number of `LooseningApprovers`.
- Any other combination (for example 2 of 3 approvers) is reported as a clear setup error before anything is sent to Jennah. The alert names both values and the two valid choices. It goes through the existing failed-pull alert and 6-hour reminder path, and the stricter config stays in force. Protection keeps working.
- The test fake rejects requests the way Jennah does (no quorum rule, or a required count that disagrees with the rule), so this class of bug fails in CI.
- The template parameter description and the install docs say what the setting can be: 1 for any one approver, or the number of approvers to require all of them.

Out of scope: N-of-M approvals (such as 2 of 3). That would need a new quorum rule in Jennah.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `guard-jennah-contract`: adds a requirement that loosening approvals carry a quorum rule Jennah accepts, derived from the stack's approvers and required count, and that an unsupported combination is reported as a setup error.

## Impact

- `internal/jennah/jennah.go`: `ApprovalRequest` gains a quorum, which is sent on create.
- `internal/guard/pull.go`: `approvalRequest` derives the quorum and returns an error for an unsupported combination.
- `internal/jennah/jennahtest/fake.go`: create validates quorum and required count like Jennah.
- Tests in `internal/guard` and `internal/jennah`.
- `guard/template.yaml` parameter description, `docs/install.md`, `docs/install-console.md`, and `CHANGELOG.md`.
- No change to stack parameters, IAM, or freeze policies. Existing installs pick it up on the next guard update.
