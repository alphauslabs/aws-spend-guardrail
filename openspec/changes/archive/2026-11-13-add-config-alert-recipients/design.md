# Design

## Context

- `config.Diff` classifies each change as `Tightening`, `Loosening`, or `Notice`. `HasLoosening` sends a version down `handleLoosening` (`internal/guard/pull.go`), which applies `config.Stricter(applied, desired)` at once, raises one Jennah approval to the stack's `LooseningApprovers`, and applies the full version after approval plus `LooseningDelay`. `config.Equal` decides whether the effective config changed.
- The guard publishes alerts to `AlertTopic` through `Alerter`; its role has only `sns:Publish` on that topic. The stack's `AlertEmail` is one `AWS::SNS::Subscription` that CloudFormation owns; the function doesn't know the address today.
- SNS email subscriptions: `Subscribe` sends a confirmation email and returns `pending confirmation` until the link is clicked; a pending subscription has no ARN and can't be unsubscribed; SNS deletes it if it isn't confirmed within a few days. Subscribing an address that is already confirmed returns the existing subscription and sends nothing.
- Guard state lives in the DynamoDB `StateTable` (`State` plus per-item records such as frozen scopes and reported accounts).

## Goals / Non-Goals

**Goals:**
- Recipients managed from config, with every change approved, and no way for config to leave a payer org with no recipient.

**Non-Goals:**
- Changing the approval mechanism itself (approvers, count, window, delay stay stack parameters).
- Confirming subscriptions on a recipient's behalf.

## Decisions

### D1. `alert_emails` is a top-level list in the document
It sits beside `excluded_accounts`, not under `defaults` or a scope, because alerts are per payer org. `Resolve` copies it, lowercased and sorted, into `Resolved.AlertEmails`, so diffing and equality don't depend on order or case. The JSON schema and `checkSemantics` both enforce the limits (shared validation, as today). 20 is enough for named people; a distribution list counts as one.

### D2. Recipient changes reuse the loosening path
`Diff` emits one change per added or removed address, `Field: "alert_email"`, with `Direction: Loosening`. Using the existing direction means `HasLoosening`, the approval, the delay, supersession, and the rejected and expired outcomes all apply unchanged. `Stricter` keeps the applied `AlertEmails` (like `Period`), so the recipients in force never change before approval. `Equal` compares `AlertEmails`. A first config is diffed against an empty one, so recipients in it are additions too and wait for approval while its scopes apply at once. `Change.String` renders `alert recipient added: x` and `alert recipient removed: x`.

The approval's title says "Loosen" today. When every awaiting change is a recipient change, the title becomes `Change alert recipients for spend guard config v<N> (payer <id>)`, so an approver isn't told protection is loosening when it isn't. The body lists the changes as now.

*Alternative:* a new direction, such as `Guarded`. Rejected: every caller of `HasLoosening` would need to learn it, for the same behavior.

*Alternative:* additions apply at once, removals need approval. Rejected: an addition can send alerts to an outsider, and the SNS confirmation doesn't help because whoever added the address can confirm it.

### D3. The guard syncs subscriptions after applying config, and only touches its own
After `evaluate` and before `Reconcile`, `Pull` calls `syncRecipients(st.Effective.AlertEmails)`. It lists the topic's subscriptions (`ListSubscriptionsByTopic`, protocol `email`) and:

- subscribes each recipient with no subscription, unless the guard asked for one in the last 24 hours that is still pending (D4);
- unsubscribes each confirmed subscription whose address is in the guard's `alert_subscriptions` record, is not a recipient, and is not the stack's `AlertEmail`;
- leaves every other subscription alone: the stack's, any added by hand, and any that existed before the guard first listed that address.

The guard's record of its own subscriptions (`State.OwnSubscriptions`: each address and when the guard last asked to subscribe it, at most 20 entries, in the existing state item) is how the guard tells its own subscriptions from others. The ARN to unsubscribe comes from the listing, not the record. An address already subscribed when it first becomes a recipient is not recorded as the guard's, so the guard never removes a subscription someone else made.

The stack passes `AlertEmail` to the function as `ALERT_EMAIL`, so the guard can refuse to unsubscribe it even if the guard's record lists it.

Sync runs on the effective recipients, so it also runs when Jennah is unreachable (it changes nothing then), and it doesn't depend on Jennah.

### D4. Pending subscriptions are re-asked at most daily
A pending subscription can't be removed, and asking SNS again sends another email. So the guard asks again for a pending address only after 24 hours, which also covers SNS deleting an unconfirmed subscription. Each re-ask is also the daily `alert_recipient_unconfirmed` report, since it only happens for an address the guard asked for a day or more ago that still isn't confirmed. If a recipient is removed while still pending, the guard stops asking; if the old link is still confirmed later, the next run finds a confirmed subscription the guard made for a non-recipient and unsubscribes it.

### D5. Events
New inbox kinds: `alert_recipient_subscribed` and `alert_recipient_removed` (one per address and run), and `alert_recipient_unconfirmed` (once a day per pending address). They carry only the address and the config version. Existing kinds keep their shape, so the layout version stays the same.

### D6. Permissions
The guard role gains `sns:Subscribe` and `sns:ListSubscriptionsByTopic` on `AlertTopic`, and `sns:Unsubscribe` on `AlertTopic`. Nothing on `NotifyTopic` or any other topic. AWS authorizes `Unsubscribe` against the topic's ARN, not the subscription's: the first design granted it on `${AlertTopic}:*`, and the e2e live check (2026-11-13) was refused with `not authorized to perform: SNS:Unsubscribe on resource: <topic ARN>`. Both grants reach the same subscriptions (every subscription on this topic, the stack's `AlertEmail` included), so the change widens nothing; the guard's code is what refuses to unsubscribe the stack's address.

## Risks / Trade-offs

- [A recipient never confirms and gets nothing.] → Daily `alert_recipient_unconfirmed` events, and the stack's `AlertEmail` still receives every alert.
- [Someone with the stack's permissions unsubscribes a config recipient by hand.] → The next run subscribes it again (the guard sees no subscription), so a confirmation email arrives; a person who wants out should be removed from the config.
- [A recipient change waits for approval, so a new on-call starts receiving alerts later.] → Accepted; the approval delay is the MSP's choice, and the stack's `AlertEmail` (often a distribution list) covers the gap.
- [An approver approves without reading the addresses.] → The approval lists each address; nothing more can be done in the guard.

## Migration Plan

A guard updated to this release with a config that has no `alert_emails` does nothing new. The stack update adds the SNS permissions and `ALERT_EMAIL`; no subscription changes. To roll back, publish a version without `alert_emails` first and let it be approved, so the guard removes its subscriptions; otherwise they stay on the topic after rollback, which is harmless but no longer managed.
