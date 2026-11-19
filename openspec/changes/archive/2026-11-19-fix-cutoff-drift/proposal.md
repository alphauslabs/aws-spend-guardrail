# Proposal

## Why

When someone detaches the AI cutoff policy from an account by hand, which the operating guide now describes for emergencies (Jennah unreachable, approvers unavailable, or a lapsed Jennah plan), the guard doesn't notice. It keeps its record that the account is cut off, so it never cuts that account off again, and the account's AI use is uncapped until the cutoff is lifted through config, which in those emergencies may not be possible for a long time. A budget freeze detached by hand is caught by the daily drift check; an AI cutoff isn't.

The reverse is also silent: the AI cutoff policy attached to an account the guard has no cutoff for, by hand or by a run whose record was lost, blocks the account's AI use with nothing in the guard's state or inbox to say why.

## What Changes

- On each AI guard run while it holds any cutoff, the guard reads which accounts the AI cutoff policy is attached to. A recorded cutoff whose account no longer has the policy is treated as lifted, with the reason "detached outside the guard": the guard forgets it, writes a `cutoff_lifted` event and a `drift` event, and alerts once. From then on the account is watched again, and cut off again only on a new breach, as after an approved lift.
- Daily, the guard alerts once per account where the AI cutoff policy is attached without a recorded cutoff, with a `drift` event, and doesn't detach it.
- The guard's role gains `organizations:ListTargetsForPolicy` on the AI cutoff policy. Template revision 7.
- The operating guide's emergency section says the guard now notices a detached cutoff and watches the account again.

## Capabilities

### New Capabilities

### Modified Capabilities

- `activity-guard`: a cutoff detached outside the guard is noticed and recorded as lifted; an attached cutoff policy the guard didn't record is reported.

## Impact

- `internal/activity` (the `Policies` interface gains the policy's attached accounts; `lifts` handles detached cutoffs), `internal/awsx` (`ListTargetsForPolicy`), `internal/guard` (the daily unrecorded-attachment check).
- `guard/template.yaml`: the permission and revision 7.
- `docs/operating.md`, `docs/ai-guard.md`; CHANGELOG.
