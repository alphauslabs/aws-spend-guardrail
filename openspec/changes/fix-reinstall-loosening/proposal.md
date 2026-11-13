# Proposal

## Why

Deleting and recreating a guard's stack deletes its state, including which config version was in force and any loosening waiting for approval. The new guard compares the latest version against nothing, so every scope counts as new protection (tightening) and the version applies at once, with no approval. A version whose loosening was pending, or was rejected, takes effect on reinstall. The 7.1 live test on e2e (2026-11-13) showed this path, and also that the reinstall leaves no trace in Jennah: the new guard's `config_applied:10` event had the same ID as the old guard's, so Jennah kept the old row.

Deleting the stack already lifts every freeze, so this is not a new way for the stack owner to loosen protection. It is a way for a loosening to skip the approval it was waiting for, and for that to happen silently.

## What Changes

- On its first run (no stored state), the guard reads the inbox for the last version a guard applied in full. If there is one, it applies that version at once, then treats the latest version as a change from it: tightening applies at once and loosening goes through approval, as for any other version. With no earlier applied version (a true first install), the latest version applies at once, as today.
- A loosening that was pending before the reinstall keeps its existing approval: the guard finds the approval ID in the earlier install's `loosening_raised` event in the inbox, instead of raising a second request. Only when that record is missing does it raise a new approval. The loosening delay restarts from the new guard's first sight of the version.
- The first run emits a new `guard_installed` event saying which version was last in force, which version is latest, and what the guard did.
- Events tied to a config version carry the install in their ID, so a reinstalled guard's events are recorded instead of being dropped as duplicates of the old guard's.
- The reader key's reach adds read access to the payer org's inbox. A reader key minted before this change can't read the inbox: the guard then alerts once, applies the latest version as today, and records the check as not done in `guard_installed`.

## Capabilities

### New Capabilities

### Modified Capabilities

- `guard-jennah-contract`: a new install resumes from the version last in force, reports itself, and records its version events; the reader key reads the inbox.

## Impact

- `internal/guard/pull.go`: first-run baseline, `guard_installed` event, install-scoped event IDs.
- `internal/guard/guard.go`: `State` gains an install ID; `ConfigSource` gains reads of the latest `config_applied` event and of a version by number.
- `schema/schema.go`: `KindGuardInstalled`; the reader credential reaches the inbox with `datastore.data:read`.
- `internal/cli` (`setup`): mints the reader with the wider reach; docs on rotating an existing reader key.
- Docs: `docs/operating.md` (reinstalling a guard), `docs/install.md` (deleting the stack), CHANGELOG with a Security-relevant entry for the reader's added read.
- Existing installs keep their state and their event ID format until reinstalled; nothing changes for them on upgrade except the reader key's reach once reminted.
