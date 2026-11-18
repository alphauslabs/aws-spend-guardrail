# Design

## Context

The activity engine (`internal/activity/engine.go`) records each cutoff in its store. `evaluate` skips an account with an attached cutoff ("already cut off"), and `lifts` deletes the record and detaches the policy when a lift is approved or the cap that was passed is raised. After a lift, an account still over its cap is cut off again only when new usage arrives (a new breach), since `evaluate` looks only at accounts with records in the current tick. Nothing compares the records with what is actually attached. Budget freezes have that comparison in the daily `Drift` (`internal/guard/freeze.go`): a freeze detached outside the guard is reported and forgotten, and a freeze policy attached without a recorded freeze is reported.

## Goals / Non-Goals

**Goals:**
- A hand-detached AI cutoff stops silently leaving its account uncapped: the account is watched again within minutes.
- An AI cutoff policy attached without a record is reported.

**Non-Goals:**
- Re-attaching a detached cutoff: the detach is taken as a deliberate lift, as for freezes.
- Detaching an unrecorded attachment: someone may have attached it on purpose.

## Decisions

### D1: Detached cutoffs are found on the minute run, only while any is held

`activity.Policies` gains `Attached(ctx, policyID) (map[string]bool, error)`, the accounts a policy is attached to. In `lifts`, when the pack holds any attached cutoff, the engine reads the policy's accounts once per tick (one `ListTargetsForPolicy` page in practice) and treats each recorded attached cutoff whose account is missing as lifted with the reason "detached outside the guard": it deletes the record without detaching, emits `cutoff_lifted` and a `drift` event, and alerts once. A read that fails is skipped for the tick (no cutoff is forgotten on a failed read). With no cutoffs held, nothing is read.

Daily was considered, as for freezes; a cutoff guards fast spend, and an account left uncapped for up to a day defeats it. The read costs one Organizations call a minute only while a cutoff is held.

### D2: Watched again like after an approved lift

Deleting the record puts the account back under `evaluate`: a new breach cuts it off with a new id. That is the same as after an approved lift. In an emergency where the account must keep working beyond its cap without a config change, turning the AI guard off is the way, as the operating guide says.

### D3: Unrecorded attachments are reported daily

The daily run reads the AI cutoff policy's accounts and, for each one without a recorded cutoff, emits a `drift` event and alerts once per account per day (the freeze check's pattern), without detaching.

### D4: Permission and revision

The guard role's `AICutoff` statement gains `organizations:ListTargetsForPolicy` on the AI cutoff policy. Template revision 7.

## Risks / Trade-offs

- **Eventual consistency.** Right after the guard attaches a cutoff, `ListTargetsForPolicy` might not show it yet, and the next tick would take the cutoff as detached. The engine skips cutoffs attached less than 5 minutes ago in this check.
- **A detach the guard itself started** (an approved lift) deletes the record first, so it never looks detached.
