# Proposal

## Why

Local alerts (freezes, rejected configs, Jennah outages, and unprotected accounts) go only to the single email address given in the `AlertEmail` stack parameter. A payer org usually has more than one person who should hear about a freeze, and an MSP with hundreds of payer orgs can't run a stack update in each one whenever its on-call changes. Today the only other option is to subscribe addresses to the alert topic by hand, outside the stack and outside any record.

Recipients belong with the rest of the guard's settings: in the config in Jennah, published once and picked up by each guard. But anyone who can change recipients can silence freeze alerts (by removing them) or send alerts outside the MSP (by adding their own address), so every recipient change needs the same approval as loosening protection.

## What Changes

- **The config gains `alert_emails`,** a list of up to 20 addresses. The guard subscribes each to the stack's alert topic and unsubscribes addresses it added that are no longer listed. Each new address confirms its subscription once.
- **Any change to `alert_emails` needs approval,** adding or removing, through the existing loosening approval: the stack's `LooseningApprovers`, its required count, window, and delay. Until approved, the recipients in force stay as they were, while the rest of the version applies as today.
- **The stack's `AlertEmail` stays, as the one recipient config can't remove.** The guard never unsubscribes it, or any subscription it didn't make, so a freeze is still reported if Jennah is unreachable or the config is tampered with.
- **The guard records recipient changes in the inbox,** including addresses still waiting for confirmation.
- **The guard's role can subscribe and unsubscribe on the alert topic only.**

Non-goals: several addresses in the `AlertEmail` stack parameter (config covers it), non-email endpoints, per-scope recipients, and approval-free additions for allowed domains (possible later if approvals for new recipients become a chore).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `guard-config`: the config document gains `alert_emails`, and a change to it is classified as needing approval. Introduced by `add-budget-freeze-guard`, not archived yet, so the delta adds requirements to it.
- `spend-freeze`: local alerts reach the config's recipients as well as the stack's `AlertEmail`, which config can't remove. Introduced by `add-budget-freeze-guard`, not archived yet, so the delta adds requirements to it.

## Impact

- **Code:** `config` (schema, document, resolve, diff, stricter, equal), `internal/guard` (recipient sync after applying a config, state for subscriptions the guard made), `internal/awsx` (SNS subscribe, unsubscribe, and list on the alert topic), `guard/main.go` (wiring the stack's `AlertEmail` and the SNS client into the guard).
- **Template:** `guard/template.yaml` (the guard role's SNS permissions on `AlertTopic`, and `AlertEmail` passed to the function).
- **Schema:** new inbox event kinds for recipient changes; the layout version is unchanged because existing events keep their shape.
- **Docs:** `docs/config.md`, `docs/operating.md`, `docs/install.md`, and `CHANGELOG.md` (Security-relevant: the new SNS permissions and that config can now direct alerts).
- **Deployed guards:** unchanged until updated; a config without `alert_emails` behaves as today.
