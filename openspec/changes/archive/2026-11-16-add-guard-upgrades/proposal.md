# Proposal

## Why

A new guard release reaches a payer org only when someone updates its stack, and today nothing helps with that. A guard doesn't know a newer release exists, nobody is told, and an MSP with hundreds of payer orgs can't see which guard runs which version without one inbox query per payer. Each CHANGELOG entry says what changed but not what to do (rerun setup, widen a key, set a new parameter, in what order). Nothing promises that a release installs cleanly over an earlier one or can be rolled back, and a stack left on an older template than its code needs fails in ways that look unrelated. The only update instructions are one bullet in `docs/install-console.md`.

## What Changes

- **Each release says how to upgrade.** Every CHANGELOG entry gets an "Upgrade" section listing the actions that release needs, or "None." The release workflow refuses a tag whose entry lacks it, like the Security-relevant rule, and the release notes include it.
- **Releases install over earlier ones.** A release reads the state, config, and inbox written by any earlier release with no manual step, unless its Upgrade section says otherwise. Rolling back to the previous release is safe unless the newer release's Upgrade section says it isn't. Test fixtures of each release's stored state keep this honest.
- **The guard reports a stack older than its code.** The template carries a revision number and the code the lowest revision it needs. A guard on an older template keeps running, alerts once, and records both in its heartbeat. A `GuardVersion` stack parameter that doesn't match the running code is recorded and alerted the same way.
- **Operators can see versions across payer orgs.** `guardrail versions --msp ID` lists each payer org's running version, code hash, and last heartbeat against the latest release, flagging guards that are behind, silent, or unknown. The console's guardrail page shows the same per install and says when a newer release is out.
- **An upgrade guide**, `docs/upgrade.md`: finding what runs, reading the Upgrade sections of every release in between, the order of steps (CLI, Jennah setup, keys, stack, check), the CLI and console commands, skipping releases, and rolling back.
- Guards never contact GitHub. Only the CLI and the console look up the latest release.

## Capabilities

### New Capabilities

- `guard-upgrades`: how releases state their upgrade steps, what an upgrade and a rollback guarantee, how a guard reports a stack or label that doesn't match its code, and how operators see which version each payer org runs.

### Modified Capabilities

## Impact

- `tools/changelogcheck` and `.github/workflows/release.yml`: the Upgrade-section rule and release notes. `CHANGELOG.md`: an Upgrade section for the unreleased entry.
- `guard/template.yaml` and `guard/main.go`: the template revision in the environment; the guard's checks, alerts, and heartbeat fields.
- `internal/guard`: state fixtures per release and a load test.
- `cmd/guardrail`, `internal/cli`, `internal/jennah`: the `versions` command, listing an MSP's inboxes, and reading the latest release from GitHub's public API.
- Jennah console (`app/` in the jennah repo): a version column on the guardrail page and a newer-release notice on a payer's page.
- Docs: `docs/upgrade.md` (new), links from `docs/install.md`, `docs/install-console.md`, and `docs/verify.md`.
- Out of scope: updating many payer stacks at once (parked with the scale work), automatic updates, and guards checking for releases themselves.
