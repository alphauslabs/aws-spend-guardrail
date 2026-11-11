# Design

## Context

- `guard/main.go` dials two Jennah clients: the reader (`Guard.Config`) and the writer (used only by the outbox consumer). Each reads its key from Secrets Manager at cold start.
- `Guard.Daily` runs at 00:15 UTC: period lifts, drift, coverage, the `heartbeat` event, then reconcile.
- Jennah `add-whoami-api-key` adds `WhoAmIResponse.api_key` (`ApiKey`: id, label, expiry), set only for an API-key caller and omitted on a server-side read error.

## Goals / Non-Goals

**Goals:**
- An operator hears about an expiring key a month ahead and again as it gets closer, without anyone watching a dashboard.

**Non-Goals:**
- Rotating keys, or warning through anything but the local alert topic and the heartbeat record.

## Decisions

### D1. The guard asks with each key, once a day
A `KeyInfo` interface on the guard, with one implementation per key (reader and writer), calls `WhoAmI` and returns the key's id, label, and expiry, or "unknown". It runs in `Daily`, before the heartbeat, so the heartbeat carries the result. Once a day is enough for day-granular thresholds and costs two calls a day.

*Alternative:* read expiry with `ListApiKeys`. Rejected: it needs `iam.apikeys:read`, which would let a guard key see every key in the enterprise.

### D2. Thresholds are crossed, not hit
The guard keeps, per key, the key id, the expiry it saw, and the smallest threshold already warned for. Each run finds the smallest threshold the remaining time is at or below and warns if that's smaller than the last one warned. Missed runs therefore send one alert for the current threshold, not one per threshold passed. A changed key id or expiry resets the record. State lives in the existing state item.

### D3. Unknown is not "never"
A missing `api_key` (an older Jennah, or its read error), or a failed call, is recorded as `unknown` and warns nothing, so a Jennah outage doesn't produce expiry alarms. `never` is only when Jennah reports the key with no expiry. The outage alerts already cover a key that has expired.

### D4. The alert names the fix
The alert reads, for example, `guardrail: reader key expires in 14 days` with the label, the expiry in UTC, and a pointer to "Rotate a key before it expires" in `docs/operating.md`.

## Risks / Trade-offs

- [A guard deployed before Jennah ships `add-whoami-api-key` always records `unknown`.] → Harmless; it starts warning once Jennah reports keys, with no guard change.
- [Two guards sharing keys would both warn.] → Each payer org has its own keys by design, so this doesn't arise.

## Migration Plan

Deploy after the Jennah change is live. Existing keys are checked on the next daily run; any already within 30 days of expiry warn at once.
