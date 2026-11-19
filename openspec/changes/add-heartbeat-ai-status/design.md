# Design

## Context

`internal/guard/heartbeat.go` builds the heartbeat's state (`snapshot`) and writes a heartbeat at the end of a run when its digest (the snapshot without volatile parts) changed (`heartbeatIfChanged`). The AI part (`aiSnapshot`) reads cutoffs and the trail's stall status from the activity store. The new state already exists in stored form:

- the block policy's state, in state meta `ai.blocked` (add-ai-blocked D5): `sets`, `attached`, `failing`, `too_large`;
- mantle read failures per region, in activity meta `mantle.status` (add-mantle-usage D5);
- accounts reported as not linked, in the reported records of kind `mantle_unlinked` (`<account>@<region>`);
- the watched regions, `FastPath.MantleRegions`;
- `ai.unknown_models`, in the config in force.

## Goals / Non-Goals

**Goals:**
- A reader of the newest heartbeat sees what is blocked and whether mantle usage is watched, without reading events.
- Changes to either write a heartbeat; nothing volatile does.

**Non-Goals:**
- A new layout version: the fields are additive.
- Per-minute mantle detail (counted values) or spend; those stay out of the heartbeat.

## Decisions

### D1: The AI section's new fields

```json
"ai": {
  "mode": "enforce",
  "cutoffs": [...],
  "trail": {"stalled": false, "last_file": "..."},
  "unknown_models": "cutoff",
  "blocked": {
    "accounts": {"730335604511": ["agents", "async_invoke"]},
    "attached": true,
    "failing": ""
  },
  "mantle": {
    "regions": [{"region": "us-east-1", "read": true}, {"region": "us-west-2", "read": false, "error": "no CloudWatch observability sink ..."}],
    "unlinked": {"us-east-1": ["905418369712"]}
  }
}
```

`blocked` and `mantle` are present only when the guard has a block policy, or watches mantle regions, respectively. `blocked.accounts` is what the guard last wrote to the policy, not the config, so it shows what is actually denied (a too-large or failing write leaves the last written sets, with `failing` saying why).

### D2: Digest

All new fields are part of the digest: they change only when the guard's blocks, a region's readability, or the unlinked accounts change. Error text for a failing region is kept in the digest too: a changed error is a change worth a heartbeat, and it changes rarely.

### D3: The console

`guardState` reads `ai.unknown_models`, `ai.blocked`, and `ai.mantle` defensively, as it does the rest. The payer page's AI section gains three short blocks: blocked kinds per account (with a note when the policy isn't attached or is failing), mantle regions with their status and the unlinked accounts, and how unpriced models are treated.

## Risks / Trade-offs

- A heartbeat from a guard before this change has none of the fields; the console shows nothing for them rather than "nothing blocked", since it can't tell.
