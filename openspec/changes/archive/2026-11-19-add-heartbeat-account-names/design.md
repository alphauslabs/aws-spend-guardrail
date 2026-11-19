# Design

## Context

`Guard.Names` (add-account-names-in-alerts D5) returns the organization's account names by id from `awsx.AccountNames`, which caches a listing for an hour and bounds each listing to 3 seconds; it returns nil when no listing has worked. The alerts use it through `alerttext.NameAccounts`. The heartbeat (`snapshot`, `digest`, `heartbeatDetail` in `internal/guard/heartbeat.go`) carries ids only. Spend figures are added in `heartbeatDetail` after the digest is taken, so they never count as a change.

## Goals / Non-Goals

**Goals:**
- A heartbeat reader can show the same `<id> (<name>)` the alerts show, for every account the state mentions.
- No extra heartbeats from names, and no failed heartbeat when names can't be read.

**Non-Goals:**
- Names in events other than the heartbeat (`mantle_unlinked` already carries one).
- Every account in the organization: a large org would bloat each heartbeat.

## Decisions

### D1: `account_names`, limited to the accounts the state mentions

```json
"account_names": {"730335604511": "e2enormal", "905418369712": "e2eother"}
```

The ids are collected from the snapshot: `account:<id>` scope targets (in or out of the config), `excluded`, `unprotected[].account`, and, in `ai`, `cutoffs[].account`, `blocked.accounts` keys, and `mantle.unlinked` values. An id with no name in the listing is left out. When `Names` is nil or returns nothing, the field is left out rather than empty, so a reader can't mistake "couldn't list" for "no names".

### D2: Added after the digest

Like spend, the names are added in `heartbeatDetail`, after the digest is taken. A rename alone writes no heartbeat; a newly mentioned account always comes with a state change that does, and that heartbeat carries its name. A listing that fails on one run and works on the next changes nothing either.

### D3: The console

`guardState` reads `account_names` defensively into an id-to-name map (empty from older guards). The payer page shows an account as `<id> (<name>)` when a name is known, else the bare id, in the scopes, excluded and unprotected accounts, AI cutoffs, blocked, and mantle unlinked lists. The events table is unchanged.

## Risks / Trade-offs

- Names can be up to an hour stale (the cache's TTL), and a rename shows only from the next heartbeat. Acceptable: names are labels, ids stay the key.
