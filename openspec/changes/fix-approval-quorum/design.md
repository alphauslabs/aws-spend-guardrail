# Design

## Context

`Client.RaiseApproval` (`internal/jennah/jennah.go`) builds `CreateApprovalRequest` with approvers and `RequiredApprovals` but no `Quorum`. Jennah's `quorumFromProto` refuses `QUORUM_UNSPECIFIED` for approvals. Jennah also refuses a nonzero `required_approvals` that disagrees with the rule (`ErrThresholdMismatch`): 1 for `ANY_ONE`, and the number of approvers for `ALL`. The SDK (v0.79.0) already has the `Quorum` field, so no dependency bump is needed.

The pull loop applies a version's tightening parts before it raises the approval (`pull.go`), and an error from the raise already goes through the failed-pull alert with 6-hour reminders. That is the path the e2e failure took.

The approval settings come from stack parameters through `guard/main.go` (`LOOSENING_APPROVERS` split on commas, `LOOSENING_REQUIRED_APPROVALS`).

## Goals / Non-Goals

**Goals:**
- Every loosening approval the guard raises is accepted by Jennah.
- An unsupported setting is reported clearly instead of as a Jennah RPC error.
- A test fails if the request is missing the quorum rule.

**Non-Goals:**
- N-of-M approvals. That needs a Jennah quorum rule.
- Rejecting a bad setting at stack create or update. CloudFormation rules can't count a comma-delimited list's members, so the check stays in the guard.
- Changing how decided approvals are read.

## Decisions

### D1. Derive the quorum in the guard, from the stack settings
`approvalRequest` (in `internal/guard`) computes the rule from `LooseningRequired` and the cleaned approver list: 1 means `ANY_ONE`, and equal to the approver count means `ALL`. `ApprovalRequest` gains a `Quorum` field, which `RaiseApproval` maps to the proto enum. For `ANY_ONE` it sends `required_approvals` as 1, and for `ALL` as the approver count. Both match Jennah's derived value. The guard owns the meaning of its stack settings, so the jennah client stays a thin mapping.

A required count of 1 with a single approver is `ANY_ONE`, not `ALL`. Both mean one approval, and `ANY_ONE` is the plain reading of "1".

### D2. Report an unsupported combination as an error on the raise, not a crash at start
`approvalRequest` returns an error such as "LooseningRequiredApprovals is 2 but LooseningApprovers lists 3: Jennah needs 1 (any one approver) or 3 (all of them). Update the stack." No request is sent. The error rides the existing failed-pull alert and reminders.

Alternative: `log.Fatalf` at start, like a missing environment variable. Rejected, because it would stop freezes, tightening, and the AI guard over a setting that only matters for loosening. Failing only the loosening keeps the stricter config, which is the safe direction.

### D3. Clean the approver list before counting
`strings.Split` on an empty or trailing-comma value gives blank entries, and a repeated address would inflate the count for `ALL`. Trim, drop blanks, and dedupe case-insensitively, keeping the first spelling, before counting and before sending. Jennah would otherwise see a different approver count than the guard used.

### D4. Make the fake behave like Jennah
`jennahtest.Server.CreateApproval` returns `InvalidArgument` for `QUORUM_UNSPECIFIED`, and for a nonzero `required_approvals` that disagrees with the rule, using Jennah's messages. Existing tests that raise approvals then fail without the fix, and keep catching this class of bug.

## Risks / Trade-offs

- [An MSP that set 2 with three approvers expecting 2 of 3 can no longer loosen anything] → It couldn't before either, since every request failed. The alert now says what to change. The docs drop the "use 2" advice in favor of "1, or the number of approvers".
- [Separation of duties for small MSPs] → With exactly two approvers, `ALL` gives the same separation the old advice meant. Larger groups who want it wait for N-of-M in Jennah.
- [The fake drifts from Jennah again] → The fake cites the Jennah function it mirrors, and the live e2e lift test exercises the real path.
