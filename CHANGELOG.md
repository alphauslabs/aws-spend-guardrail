# Changelog

Every release has an entry here. Each entry has a **Security-relevant** section that lists changes to the freeze policies, the IAM permissions the stack creates, the stack parameters that control approvals, and the guard's outbound network calls, or says "None." A release without such an entry is refused by the release workflow.

## [Unreleased]

### Added
- Config field `alert_emails`: up to 20 addresses that receive local alerts besides the stack's `AlertEmail`. The guard subscribes and unsubscribes them on the alert topic, and records each change in the inbox (`alert_recipient_subscribed`, `alert_recipient_removed`, and a daily `alert_recipient_unconfirmed`). Any change to the list needs the loosening approval.
- Once a day the guard reads its reader and writer keys' expiry from Jennah and alerts locally 30, 14, 7, 3, and 1 days before a key expires. The heartbeat event lists both keys' labels and expiry. Needs Jennah v0.192.0 or later; with an older Jennah the expiry is recorded as unknown and nothing is alerted.
- A loosening approval shows the config version's note as the reason given (or says none was given), carries it in its payload, and the `loosening_raised` event records it.

- The AI guard (opt-in with `EnableAIGuard`): reads the organization CloudTrail trail every minute, prices Bedrock calls from their token counts, and cuts off an account's AI access when it passes an hourly, daily, or monthly cap, or calls a model or region not allowed, uses a Bedrock API key without being allowed to, or changes Bedrock's logging settings. Configured in the config's new `ai` section; lifts go through the loosening approval. Writes cutoff, lift, trail-stall, and hourly usage events. See `docs/ai-guard.md`.
- `guardrail preflight --ai` checks that the organization trail can feed the AI guard.
- Shared AI prices: `guardrail prices publish` writes a versioned price table, built from the AWS Price List API, to the enterprise's `guardrail.prices` dataset, which `setup` creates once per enterprise. Guards read it on the 5-minute pull and merge it with their release's table tighten-only, record each version they start using as `prices_applied`, and name the price version in AI cutoff events. `guardrail prices build` regenerates the release's table. The MSP id `prices` is reserved.

### Security-relevant
- New SCP, created only with `EnableAIGuard=true`: `guardrail-ai-cutoff-<msp id>`, denying Bedrock model invocation, Bedrock API keys, and AgentCore invoke and session actions. The guard attaches it to accounts (never OUs) itself, so its role gains `organizations:AttachPolicy` and `DetachPolicy` on that policy and on the organization's accounts.
- With `EnableAIGuard=true`, the guard's role can list and read the organization trail's bucket (`s3:ListBucket`, `s3:GetBucketLocation`, and `s3:GetObject` under `AWSLogs/`), and, when `TrailKmsKeyArn` is set, `kms:Decrypt` on that key through S3 only. Raw trail records and prompts are not sent to Jennah; only cutoffs and hourly aggregates are.
- New stack parameters `EnableAIGuard`, `TrailBucket`, `TrailPrefix`, and `TrailKmsKeyArn`. The state table now expires items with an `expires` attribute (usage counters, after about 40 days).
- New outbound call: once a day, `WhoAmI` to the Jennah endpoint the guard already uses, once with each of its keys. It needs no permission beyond the key and returns only that key's metadata.
- Config in Jennah can now direct local alerts to email addresses. Adding or removing an address needs the same approval as a loosening, from the approvers set in the stack, including in a first config. Config can't remove the stack's `AlertEmail`, a subscription made outside the guard, or one that existed before config listed it.
- The guard's role can subscribe, unsubscribe, and list subscriptions on the stack's alert topic (`sns:Subscribe` and `sns:ListSubscriptionsByTopic` on the topic, `sns:Unsubscribe` on its subscriptions), and no other topic.
- The guard function receives the stack's `AlertEmail` as `ALERT_EMAIL`, so it can refuse to unsubscribe it.
- The guard's reader key now also reaches the enterprise's `guardrail.prices` dataset, read-only. Shared prices can only raise a price or add a model, never lower one, so whoever can write that dataset can make the AI guard stricter but not looser. Existing reader keys need reminting with the added dataset; until then the guard alerts once and uses its release's prices.

### Fixed
- `guardrail preflight` reported an account or OU as having no free SCP slot at 5 attached policies. AWS now allows 10, so the check and the install docs use 10.
- The guard rewrote an enforcing scope's freeze action whenever any part of the scope changed, for example only its limit. It now writes the action only when its threshold, policy, target, execution role, approval model, or subscribers differ from config.
- The guard no longer replaces each budget's notification on every run. AWS Budgets omits a percentage threshold type from what it returns, so the guard saw every notification as changed, and replacing it reset the budget's calculated spend to zero, which kept spend from ever reaching the threshold.

## [v0.0.1] - 2026-11-10

Test release of the release workflow. Not for deployment.

### Security-relevant
- First implementation. The freeze policies, IAM roles, and stack parameters it creates are described in docs/install.md.
- The guard's role can read its own Lambda configuration (`lambda:GetFunction`, on its own function only) to report its code hash.
- The guard's role can list the organization's accounts and their parents (`organizations:ListAccounts`, `organizations:ListParents`) for the coverage check.
- Optional stack parameters `ExpectedPayerAccountId` (refuses the stack in any other account) and `ReactToAccountEvents` (adds an EventBridge rule that invokes the guard).
