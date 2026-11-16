# Design

## Context

Releases come from `vX.Y.Z` tags (`.github/workflows/release.yml`, add-verifiable-releases): `tools/changelogcheck` refuses a tag whose CHANGELOG entry has no Security-relevant section, and prints the entry as release notes. The guard's version is built in (`main.version` via ldflags) and reported as `guard_version` with `code_sha256` in heartbeats and `config_applied` events; the stack's `GuardVersion` parameter only becomes the `GUARD_VERSION` environment variable, which nothing reads. Stored state is JSON in the stack's DynamoDB table (`awsx.State`, meta items); Jennah layouts are additive by rule (`schema` package comment) and versioned (`LayoutVersion`, `PricesLayoutVersion`); the heartbeat carries `state_layout`. `jennah.Client.LatestEvent` reads a dataset's newest event of a kind. The console's guardrail page lists installs (`findInstalls`) and a payer page shows the heartbeat's `guardVersion`.

## Goals / Non-Goals

**Goals:** an operator knows which payer orgs run old guards and exactly what upgrading each takes; upgrades and one-step rollbacks are safe by default and tested; a half-done upgrade (package without template, or a stale label) is reported, not silent.

**Non-Goals:**
- Updating many stacks at once, or automatic updates. Parked with the scale work.
- Guards learning of releases (an outbound call from the payer account that reviewers would flag).
- Supporting rollback across more than one release.

## Decisions

**D1. "Upgrade" is a required CHANGELOG section.** `changelogcheck` requires a non-empty `### Upgrade` in the version's entry, as it does `### Security-relevant`, and `-notes` prints it first. Its content: numbered steps in order (update the CLI, rerun `guardrail setup`, change a key's reach, set a parameter, update the stack), or "None." It also states when a rollback to the previous release is not supported (D3). The current `[Unreleased]` entry gains one, written for v0.1.0 as a first install ("None; first release").

**D2. Template revision.** The template sets `TEMPLATE_REVISION` (a plain integer, starting at 1) in the function's environment. The code has `MinTemplateRevision`. A change that needs a new template resource, permission, parameter, or environment variable bumps both. A test reads `guard/template.yaml` and fails unless its revision equals `MinTemplateRevision`, so a release always ships them matched. At cold start the guard compares them; when the template's is lower, or missing (a template from before this change counts as 0), it records `template_revision` and `template_revision_needed` in the heartbeat and alerts once per pair through a state meta item: "guardrail: stack template is older than this guard needs". It keeps running; a feature whose resources are missing fails with its own error, as today. A higher template revision than the code needs is fine (rollback, D3).

**D3. Compatibility rules and fixtures.** Stored state and inbox events change additively: new fields are optional, an absent field means the old behavior, and no field changes meaning. A change that can't follow that adds migration code run on first load, and says in its Upgrade section whether rolling back past it is supported. To keep this honest, each release commits a fixture of the items its guard stores in DynamoDB (`internal/awsx/testdata/state/<version>.json`), written through the real store code (`awsx.State` and `awsx.ActivityStore` against the package's fake DynamoDB) by `TestWriteStateFixture` with `GUARDRAIL_WRITE_STATE_FIXTURE=<version>`: a config state with a pending approval, a freeze, a reported account, an AI cutoff, usage, and meta. `TestFixtures` loads every fixture into the current code and checks the pending approval, freeze, cutoff, and usage survive. The fixtures live in `awsx`, not `internal/guard`, because `awsx` imports `guard` and the stored item format is `awsx`'s; this was settled during implementation. The release workflow also checks a rollback: it checks out the previous tag in a worktree, copies the new tag's fixture into it, and runs the previous tag's `TestFixtures`; a previous tag without `TestFixtures` (before this change) is skipped.

**D4. Version label.** `GUARD_VERSION` is compared with the built-in version at cold start. When they differ and neither is `dev`, the heartbeat records `stack_guard_version` and one alert is sent per pair. The guard keeps reporting the built-in version as `guard_version`. The parameter stays, since removing it would make every existing stack update fail on an unknown parameter.

**D5. Latest release lookup, outside the guard.** The CLI and console read `https://api.github.com/repos/alphauslabs/aws-spend-guardrail/releases/latest` (public, unauthenticated, 60 requests an hour per address), taking `tag_name`. The CLI accepts `--latest vX.Y.Z` to skip the lookup (offline or rate-limited). The console caches it for an hour server side. A failure yields "unknown" for the comparison and never fails the listing. Version comparison is semantic on `vX.Y.Z`; anything else (`dev`, a commit hash) is "unknown".

**D6. `guardrail versions --msp ID [--payer ACCOUNT] [--latest vX.Y.Z] [--json]`.** Lists datasets named `guardrail.<msp>.<12 digits>.inbox` (paging through the dataset list), reads each one's newest heartbeat with `LatestEvent`, eight at a time, and prints payer, version, code hash, heartbeat time, and status: `current`, `behind`, `silent` (newest heartbeat over 2 days old), or `unknown` (no heartbeat, or an unparseable version). Behind payers are listed first. `--json` prints the same rows for scripts. It needs only `datastore.data:read` on the inboxes, which operators already have.

**D7. Console.** On the guardrail page, the installs table gains a Version column from each install's newest heartbeat (already read for the payer page, so the same helper), with "behind" next to installs below the latest release. A payer page shows "Release vX.Y.Z is available" with a link to the upgrade guide on GitHub when behind. These are console-only changes in the jennah repo, made directly.

**D8. Upgrade guide.** `docs/upgrade.md`: 1) find what runs (`guardrail versions`, or the console); 2) read the Upgrade section of every release after it, in order, and the Security-relevant sections (`docs/verify.md`); 3) update the CLI; 4) do the Jennah steps (setup, key reach); 5) update the stack with the release's template and package (CLI command and console steps, replacing the single bullet in `install-console.md`); 6) check the heartbeat's version and hash; 7) rollback: update the stack back with the previous release's template and package, allowed one release back unless the Upgrade section says otherwise. Skipping releases is fine when every skipped release's Upgrade steps are applied in order.

## Risks / Trade-offs

- An unauthenticated GitHub API limit of 60 an hour per address can be hit by many CLI runs from one office. Mitigation: `--latest`, and one lookup per run, not per payer.
- Fixtures add a step to each release. The release workflow fails if the tag's fixture is missing, so it isn't forgotten.
- A heartbeat is written daily and on state changes, so a guard updated a few minutes ago may still show the old version until its next state change or daily run. The guide says to check after the next run, and the guard writes a heartbeat on its first run after a code change (the code hash in state differs).

## Migration Plan

Stacks from before this change have no `TEMPLATE_REVISION`; their guards (once updated to code from this change) alert that the template is older, which is correct: updating the stack with the release's template clears it.
