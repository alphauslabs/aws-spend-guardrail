# Tasks

## 1. Fake behaves like Jennah

- [x] 1.1 In `internal/jennah/jennahtest/fake.go`, have `CreateApproval` return `InvalidArgument` for `QUORUM_UNSPECIFIED` ("A quorum rule is required: ANY_ONE or ALL.") and for a nonzero `required_approvals` that disagrees with the rule (1 for `ANY_ONE`, the approver count for `ALL`), citing Jennah's `quorumFromProto` and the `ErrThresholdMismatch` check (design D4); confirm the existing approval tests now fail

## 2. Send the quorum rule

- [x] 2.1 Add a `Quorum` field to `jennah.ApprovalRequest` and map it in `RaiseApproval` to `QUORUM_ANY_ONE` or `QUORUM_ALL`, with `required_approvals` of 1 or the approver count (design D1)
- [x] 2.2 In `internal/guard/pull.go`, clean the approver list (trim, drop blanks, dedupe case-insensitively, keep the first spelling) and derive the quorum in `approvalRequest`: 1 means `ANY_ONE`, the approver count means `ALL`, and anything else is an error naming both values and the two that work, with no request sent (design D2, D3)
- [x] 2.3 Tests: in `internal/jennah`, a raise with each rule is accepted and one without a rule is refused; in `internal/guard`, required 1 of 3 raises `ANY_ONE`, 2 of 2 raises `ALL`, 2 of 3 raises nothing and the pull error names "1 or 3" while a tightening in the same version still applies, and a blank or repeated approver is not counted

## 3. Docs and build

- [x] 3.1 Update the `LooseningRequiredApprovals` description in `guard/template.yaml` and the rows in `docs/install.md` and `docs/install-console.md`: 1 for any one approver, or the number of approvers to require all of them; separation of duties means two approvers and 2
- [x] 3.2 Add a `CHANGELOG.md` entry under Fixed (loosening approvals were refused by Jennah for want of a quorum rule, so no loosening could apply), with Security-relevant noting the approval rule the guard now requests; no em-dashes, Oxford comma
- [x] 3.3 Run `goimports`, `make`, and `go test ./...`; verify all pass

## 4. Live check

- [x] 4.1 With the e2e guard on this build, confirm the pending version 3 (e2estandard03 ceiling 4 to 8 USD) raises an approval to the stack's approver, a `loosening_raised` event is recorded, and after approval and the 30-minute delay the freeze SCP is detached and a `lift` event is recorded
