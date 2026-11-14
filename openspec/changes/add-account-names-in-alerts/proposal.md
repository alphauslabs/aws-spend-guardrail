# Proposal

## Why

The guard's alert emails name accounts by id only: "Account 730335604511 is cut off". An MSP with dozens or hundreds of member accounts per payer org can't tell from the email which client or workload that is without opening the AWS console. The organization already knows every account's name, and the guard's role can already read it.

## What Changes

- Every local alert (subject and body) shows each account id that is an account in the organization as `<id> (<account name>)`, for example `Account 730335604511 (e2enormal) is cut off`.
- Names are added in one place, the guard's alert sink, so every alert gets them, the budget guard's and the AI guard's, without changing each alert's text.
- Names come from `organizations:ListAccounts`, which the guard role already has, cached per function instance and refreshed hourly. If the listing fails or is slow, the alert goes out at once with bare ids.
- Only standalone ids of known accounts are named. Ids inside ARNs, cutoff ids, and the `Error:` line are left exactly as they are.
- When names would push the subject past SNS's 100-character limit, the subject keeps bare ids; the body still names them.
- Inbox events are unchanged: they keep identifying accounts by id only.

## Capabilities

### New Capabilities

### Modified Capabilities

- `spend-freeze`: local alerts name the accounts they mention.

## Impact

- `internal/alerttext`: a pure function that adds names to alert text by the rules above.
- `internal/awsx` (`Alerts`): applies it before publishing, with a cached account-name source; `guard/main.go` wires it to the organization listing.
- No new permission, stack parameter, or event field. Docs: `docs/operating.md` (alerts), CHANGELOG.
