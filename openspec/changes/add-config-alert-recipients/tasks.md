# Tasks

## 1. Config

- [x] 1.1 Add `alert_emails` to `config/schema.json` and `Document`, with `checkSemantics` refusing invalid addresses, case-insensitive duplicates, and more than 20; `Resolve` lowercases and sorts it into `Resolved.AlertEmails` (design D1); verify schema and semantic tests for a valid list, each refusal, and an absent field
- [x] 1.2 In `config/diff.go`, emit a `Loosening` change per added or removed address, keep the applied recipients in `Stricter`, compare them in `Equal`, and render them in `Change.String` (design D2); verify diff tests for add only, remove only, a reorder or case change (no change), and a mix with a tightening
- [x] 1.3 Add an `alert_emails` example to `config/examples`; verify the examples test still validates every example

## 2. Guard

- [x] 2.1 Title the approval `Change alert recipients for spend guard config v<N> (payer <id>)` when every awaiting change is a recipient change (design D2); verify pull tests for that title and for the existing title on a mixed version
- [x] 2.2 Add an SNS subscriptions interface (list, subscribe, unsubscribe on the alert topic) and the `State.OwnSubscriptions` record (design D3); verify the fakes in `fakes_test.go` model pending and confirmed subscriptions
- [x] 2.3 Implement `syncRecipients` and call it in `Pull` after `evaluate` and before `Reconcile`, with the stack's address from settings (design D3, D4); verify guard tests: a new recipient is subscribed once, a pending one is not re-asked within 24 hours and is after, a removed recipient the guard subscribed is unsubscribed, the stack's address and a hand-made subscription are never unsubscribed, an address already subscribed before it was listed is not recorded as the guard's, a removed-while-pending address confirmed later is unsubscribed, and nothing changes while Jennah is unreachable
- [x] 2.4 Emit `alert_recipient_subscribed`, `alert_recipient_removed`, and daily `alert_recipient_unconfirmed` events with new kinds in `schema` (design D5); verify the events in the 2.3 tests

## 3. AWS and wiring

- [x] 3.1 Implement the SNS subscriptions interface in `internal/awsx`, and pass `ALERT_EMAIL` into the guard's settings in `guard/main.go`; verify a unit test with a fake SNS client for listing (including `PendingConfirmation` entries) and unsubscribe by ARN
- [x] 3.2 In `guard/template.yaml`, add `sns:Subscribe` and `sns:ListSubscriptionsByTopic` on `AlertTopic` and `sns:Unsubscribe` on `${AlertTopic}:*` to the guard role, and `ALERT_EMAIL: !Ref AlertEmail` to the function's environment (design D6); verify the template validates

## 4. Docs

- [x] 4.1 Document `alert_emails` in `docs/config.md` (limit, that every change needs approval, that each address confirms once), and in `docs/operating.md` how recipients are synced, which subscriptions the guard never touches, and the unconfirmed events; mention in `docs/install.md` that `AlertEmail` is the recipient config can't remove; follow the docs' style (no em-dashes, Oxford comma)
- [x] 4.2 Add a `CHANGELOG.md` Unreleased entry, with Security-relevant listing the new SNS permissions on the alert topic and that config can now direct alerts, with every change approved

## 5. Build

- [x] 5.1 Run `goimports`, `make`, and `go test ./...`; verify all pass

## 6. Verify on the e2e payer org

- [x] 6.1 After the freeze tests are done, deploy this release to the e2e stack and publish a version adding a second address; confirm an approval is raised with the recipient title, nothing is subscribed before approval, and after approval and the delay the address gets one confirmation email and, once confirmed, a test alert
- [ ] 6.2 Publish a version removing it; after approval confirm the guard unsubscribes it and the stack's `AlertEmail` stays subscribed; confirm `sns:Unsubscribe` on `${AlertTopic}:*` is enough for the call to succeed
- [ ] 6.3 Restore the e2e config to its earlier recipients and remove any leftover test subscriptions
