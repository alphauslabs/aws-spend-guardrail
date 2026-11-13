# Proposal

## Why

Each guard runs on two Jennah API keys, a reader and a writer, and the documented default is to mint them with a one-year expiry. When a key expires, the guard can no longer read config or record events. Freezes already in force keep working, and the guard alerts that config refresh is failing, but only after the key has expired. Nobody is told beforehand, so a forgotten rotation becomes an outage in every payer org whose keys were minted together.

Jennah's `WhoAmI` is gaining the calling key's metadata, including its expiry (Jennah change `add-whoami-api-key`), so the guard can read each key's expiry with the key itself and warn weeks ahead.

## What Changes

- **Once a day, the guard reads each key's expiry** through `WhoAmI` with that key, and records both in the heartbeat event.
- **It alerts locally before a key expires:** at 30, 14, 7, 3, and 1 days before, once per threshold per key, naming the key, its label, the expiry, and the rotation steps. A rotated key (a later expiry, or a different key id) starts the thresholds over.
- **A key with no expiry or an unknown expiry raises no warning.** The heartbeat says which, so a reviewer can tell "never expires" from "couldn't tell".
- **The docs** describe the warnings in the rotation section.

Non-goals: rotating keys automatically (minting needs a person signed in to Jennah), warnings from Jennah itself, and warning about other credentials.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `guard-jennah-contract`: the guard reports and warns about its keys' expiry. Introduced by `add-budget-freeze-guard`, not archived yet, so the delta adds requirements to it.

## Impact

- **Depends on:** Jennah `add-whoami-api-key` deployed, and `jennah-sdk-go` with `WhoAmIResponse.api_key`.
- **Code:** `internal/jennah` (a `WhoAmI` call returning the key's id, label, and expiry), `internal/guard` (the daily check, warning state, heartbeat fields), `guard/main.go` (the writer client passed to the guard as well as the outbox consumer).
- **Outbound calls:** one `WhoAmI` per key per day to the Jennah endpoint the guard already calls. Listed in the changelog's Security-relevant section, as every new outbound call is.
- **Docs:** `docs/operating.md`, `CHANGELOG.md`.
- **No new permissions:** `WhoAmI` needs only the key.
