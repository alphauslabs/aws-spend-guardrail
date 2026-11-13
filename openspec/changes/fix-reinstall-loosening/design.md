# Design

## Context

See proposal.md for why. The relevant current behavior:

- `Guard.Pull` loads `State` from DynamoDB, which the stack owns, so deleting the stack deletes it. A fresh `State` has `AppliedVersion` 0 and `Effective` nil.
- `evaluate` diffs the latest version against `st.Effective`. Against nil, every scope is an added scope, which is tightening, so `HasLoosening` is false and `applyFull` runs.
- Approval request keys are `guardrail:<payer>:<msp>:v<N>`, and Jennah returns the existing approval for a known request key. A reinstalled guard raising approval for the same version already gets the old approval back.
- Event IDs for config versions are `<kind>:<N>` (`config_applied:10`). The client treats an already-committed ID as success, so a second install's event with the same ID is dropped.
- The reader key reaches the config dataset and `guardrail.prices` with read and approval permissions. The writer key can only write the inbox. Nothing the guard holds can read the inbox today; the CLI's `appliedVersion` reads it with the operator's login.

## Goals / Non-Goals

**Goals:** a reinstall can't apply a loosening that wasn't approved, and a reinstall is visible in the inbox.

**Non-Goals:**
- Stopping the stack owner from loosening protection by deleting the stack. Deleting it lifts every freeze, by design.
- Carrying other state across a reinstall (frozen records, reported accounts, own subscriptions). Those are rebuilt from AWS as today.
- Changing anything for an install that keeps its state.

## Decisions

**D1. The baseline is the last `config_applied` event in the inbox.** A first run (`AppliedVersion` 0, `Effective` nil, no `InstallID`) reads the newest `config_applied` event, then reads that version's document from the config dataset by number, and applies it with `applyFull`. `evaluate` then runs as usual against the latest version, so loosening goes through `handleLoosening`. Alternatives: (a) require approval for the whole latest version on any reinstall with history; rejected because nothing would be in force while waiting, leaving the payer unprotected. (b) Use the strictest of all published versions as the baseline; rejected because it would bring back long-gone scopes and ceilings and freeze accounts nobody expected.

**D2. The reader key gains `datastore.data:read` on the inbox.** It already reads config and prices and manages approvals; the inbox holds the guard's own events, in the same payer org. The writer key stays write-only, so the reader still can't write anything. Alternative: let the writer read the inbox; rejected because the writer is the key a compromised function would most likely misuse, and reads belong to the reader by the existing split.

**D3. Without inbox reach, fall back to today's behavior and say so.** A reader minted before this change gets a permission error on the inbox read. The guard alerts once (subject `guardrail: reinstall check not done`), applies the latest version as today, and writes `guard_installed` with `check: not_done`. This keeps a reinstall from leaving a payer unprotected because of an old key, and is no worse than today. Alternative: refuse to apply anything until the key is reminted; rejected for the same unprotected-payer reason.

**D4. An install ID scopes version event IDs.** The first run sets `State.InstallID` to the first run's Unix seconds. While `InstallID` is set, version events use `<kind>:<N>:i<InstallID>`; with it unset (every existing install, whose state predates this change), they keep `<kind>:<N>`, so an upgrade changes no IDs and retries stay idempotent. `appliedVersion` in the CLI and D1 both read by kind ordered by time, so they see either form. Alternative: always add a suffix; rejected because an upgraded guard would write a duplicate `config_applied` for the version already in force.

**D5. `guard_installed` is written once per install, when the check finishes.** Its ID is `guard_installed:i<InstallID>`. Its detail: `previous_applied_version` (0 for none), `latest_version`, and `outcome` (`resumed`, `fresh`, or `not_done`). If Jennah is unreachable on the first run, nothing is applied (as today, the config-refresh alert covers it) and the check runs on the next run that reaches Jennah; `State.InstallChecked` records that it has run.

**D6. The reused approval keeps its request key; the delay restarts.** No change to the request key. `Pending.FirstSeen` is the new guard's first sight, so the delay is counted from the reinstall. This is stricter than the old guard's timing, never looser.

## Risks / Trade-offs

- [Whoever can write the inbox can choose the baseline: a forged `config_applied` for a looser version would apply on the next reinstall.] → The inbox is written only by the guard's writer key and enterprise admins who can already change data in Jennah; the forged version must still exist in the config dataset. Recorded in docs as the trust the reinstall check places in the inbox.
- [The baseline document is read at reinstall time: a version deleted from the config dataset can't be resumed.] → Config versions are never deleted (no retention, no delete in the CLI). If the read fails, the guard treats it like D3: alert, apply the latest, and record `not_done`.
- [An inbox event lost before delivery means the baseline is an older version.] → The outbox retries until Jennah accepts; an older baseline is stricter or the same in practice, and the loosening path then asks for approval for the difference.

## Migration Plan

Ship in a guard release. Run `guardrail setup` (or the documented rotation) to remint each payer org's reader key with the inbox in its reach; until then, only a reinstall is affected, and it alerts. Rollback is the previous release: it ignores `InstallID` and `InstallChecked` in state, and reads event IDs of either form.
