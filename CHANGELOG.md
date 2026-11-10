# Changelog

Every release has an entry here. Each entry has a **Security-relevant** section that lists changes to the freeze policies, the IAM permissions the stack creates, the stack parameters that control approvals, and the guard's outbound network calls, or says "None." A release without such an entry is refused by the release workflow.

## [Unreleased]

### Security-relevant
- The guard's role can read its own Lambda configuration (`lambda:GetFunction`, on its own function only) to report its code hash.
- The guard's role can list the organization's accounts and their parents (`organizations:ListAccounts`, `organizations:ListParents`) for the coverage check.
- New optional stack parameters: `ExpectedPayerAccountId` (refuses the stack in any other account) and `ReactToAccountEvents` (adds an EventBridge rule that invokes the guard).
