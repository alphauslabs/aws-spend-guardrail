# Changelog

Every release has an entry here. Each entry has a **Security-relevant** section that lists changes to the freeze policies, the IAM permissions the stack creates, the stack parameters that control approvals, and the guard's outbound network calls, or says "None." A release without such an entry is refused by the release workflow.

## [Unreleased]

### Added
- Config field `alert_emails`: up to 20 addresses that receive local alerts besides the stack's `AlertEmail`. The guard subscribes and unsubscribes them on the alert topic, and records each change in the inbox (`alert_recipient_subscribed`, `alert_recipient_removed`, and a daily `alert_recipient_unconfirmed`). Any change to the list needs the loosening approval.

### Security-relevant
- Config in Jennah can now direct local alerts to email addresses. Adding or removing an address needs the same approval as a loosening, from the approvers set in the stack, including in a first config. Config can't remove the stack's `AlertEmail`, a subscription made outside the guard, or one that existed before config listed it.
- The guard's role can subscribe, unsubscribe, and list subscriptions on the stack's alert topic (`sns:Subscribe` and `sns:ListSubscriptionsByTopic` on the topic, `sns:Unsubscribe` on its subscriptions), and no other topic.
- The guard function receives the stack's `AlertEmail` as `ALERT_EMAIL`, so it can refuse to unsubscribe it.

### Fixed
- The guard no longer replaces each budget's notification on every run. AWS Budgets omits a percentage threshold type from what it returns, so the guard saw every notification as changed, and replacing it reset the budget's calculated spend to zero, which kept spend from ever reaching the threshold.

## [v0.0.1] - 2026-11-10

Test release of the release workflow. Not for deployment.

### Security-relevant
- First implementation. The freeze policies, IAM roles, and stack parameters it creates are described in docs/install.md.
- The guard's role can read its own Lambda configuration (`lambda:GetFunction`, on its own function only) to report its code hash.
- The guard's role can list the organization's accounts and their parents (`organizations:ListAccounts`, `organizations:ListParents`) for the coverage check.
- Optional stack parameters `ExpectedPayerAccountId` (refuses the stack in any other account) and `ReactToAccountEvents` (adds an EventBridge rule that invokes the guard).
