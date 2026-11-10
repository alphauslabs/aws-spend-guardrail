# Proposal

## Why

The guard writes every event, including a heartbeat each day, to its payer org's inbox in Jennah, and nothing ever removes them. Each inbox grows for as long as the guard runs, for every payer org of every MSP. Jennah tables can now carry a row retention (a timestamp column and a number of days, applied by the data plane in the background), so the inbox can trim itself without the guard or an operator deleting rows.

## What Changes

- **The inbox's `events` table declares a 400-day retention on `occurred_at`.** An event is removed some time after it is 400 days old, typically within three more days. 400 days keeps a full year of history plus a month of margin for annual reviews.
- **`guardrail setup` applies the retention to inboxes that already exist.** Today setup re-declares a table only when a column is missing. It will also re-declare when the table's retention differs from the declared one, and say so, because setting a retention on an existing inbox removes events already older than 400 days.
- **The docs say how long events are kept,** that the limit is a lower bound, and how to keep an older event elsewhere before it expires.
- **The Jennah SDK is updated** to the release that carries the retention field.

Non-goals: a different retention per event kind, a configurable retention, retention on the config dataset (config versions are the record of what was enforced and stay forever), and any change to the guard Lambda.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `guard-jennah-contract`: the inbox table declares a retention, and setup applies it to existing inboxes. This capability is introduced by `add-budget-freeze-guard`, which is not archived yet, so the delta adds requirements to it.

## Impact

- **Code:** `schema` (the inbox declaration), `internal/cli` (setup's re-declare check and its output), `internal/jennah/jennahtest` (the fake keeps and reports a declared retention), `go.mod` and `go.sum` (`jennah-sdk-go` v0.78.0).
- **Docs:** `docs/operating.md`.
- **Deployed inboxes:** unchanged until an operator runs `guardrail setup` again with this release. The guard itself is unaffected: it only writes events, and the table's columns do not change, so the layout version stays the same.
- **Jennah:** needs a deployment with dataset row retention (jennah `v0.191.0` or later).
