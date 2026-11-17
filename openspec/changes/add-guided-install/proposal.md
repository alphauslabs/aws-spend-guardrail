# Proposal

## Why

Most people who install the guard won't be technical. Today an install takes eight steps across the AWS console, the `guardrail` CLI, and `jnh`: download the release, make a bucket and upload two files, mint two Jennah keys, paste each into Secrets Manager and copy its ARN, run `guardrail setup` and `guardrail install-link`, publish a first config, and only then create the stack. The quick-create link (add-install-link) fills in the form, but everything before it still needs a terminal.

Jennah's console will offer a guided setup, with a Next button per step, that does the Jennah side itself (companion change `add-guardrail-guided-setup` in the jennah repository). For that guide to reach the AWS side without a terminal, the guard has to meet it halfway in three places: where the release files come from, how the two keys get into the payer account, and how soon the guard says what it found.

## What Changes

- **Hosted release files.** The release workflow also uploads each release's `template.yaml` and `guard.zip`, unchanged, to a public, read-only S3 bucket we own in us-east-1, under `guard/<version>/`. A quick-create link can then point there, so nobody has to download and upload files. The template still takes the package location as parameters and names no bucket of ours, so a client can still deploy from their own bucket, from a package they verified or built.
- **An install code.** A new optional, hidden (`NoEcho`) stack parameter, `InstallCode`, carries both Jennah keys in one value: `jgi1,<reader key>,<writer key>`. When it's set, the stack creates the two Secrets Manager secrets itself, so the person pastes one value into one field instead of creating two secrets and copying their ARNs. `InstallCode` and the two secret ARN parameters are mutually exclusive; the ARNs stay as the path for pipelines and for anyone who manages their own secrets.
- **The guard says what it found on its first run.** A new install runs the coverage check at once instead of waiting for the daily run at 00:15 UTC, so the organization's accounts appear as `unprotected_account` events within minutes. Its heartbeats gain an `org` section saying whether service control policies are enabled on the root and which scope targets have no free SCP slot, the check `guardrail preflight` makes today. The guide reads both to confirm its steps and to offer the accounts for a first cap.
- **Template revision.** The template changes (the new parameter, the two conditional secrets, and read-only Organizations permissions for the SCP checks) take the next template revision after those claimed by changes in flight (`add-regional-prices` claims 4).

Not in this change: enrollment without a pasted code (the stack proving its account to Jennah to fetch its keys), the AI guard in the guided setup, creating an organization trail, and turning SCPs on from the stack. The guard still never enables SCPs itself.

### Reverses a non-goal of add-install-link

`add-install-link` lists "hosting the template or package in an Alphaus-owned bucket" as a non-goal, so clients deploy what they verified. This change reverses that for the guided path only: hosted files are the default the guide links to, byte-identical to the signed release assets, and the running guard's reported code hash lets anyone check it against the release. The own-bucket path is unchanged and stays documented for clients who require it.

## Dependencies and sequencing

Before this change (this repository):

- **`add-install-link`** must be archived first. It introduces `guard-install`, which this change modifies, and the `install-link` command it extends.
- **`add-mantle-usage`** and **`add-regional-prices`** should land first. Both edit `guard/template.yaml` and `internal/guard`, and `add-regional-prices` claims template revision 4. This change takes the next free revision once they're in.

Waiting on this change (jennah repository, `add-guardrail-guided-setup`):

- **Part one** (provisioning, the setup record, guide steps 1, 2, 5, and 6, the in-progress list) does not wait for this change. It imports this repository's `schema` and `config` packages at any current release tag.
- **Part two** (the install code step and "Open in AWS" with hosted files) starts only after this change is released, and after Jennah's `add-service-accounts`. It needs the `InstallCode` parameter, the hosted bucket's name (decided here, in task 1.2), the first-run report, and a bump of the vendored module to the release that ships them.

Nothing in this change depends on Jennah's changes. It can be released, and archived, as soon as it's done.

Overall order:

1. Now: `add-mantle-usage` and `add-regional-prices` here; part one of `add-guardrail-guided-setup` and `add-service-accounts` in jennah.
2. Next: archive `add-install-link`, then implement and release this change.
3. Then: part two of `add-guardrail-guided-setup`.
4. Later, as separate changes: enrollment without a pasted code, and the AI guard step in the guided setup.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `guard-install` (introduced by `add-install-link`, which must be archived first): the stack accepts an install code and creates the secrets from it; a link may point at the hosted release files.
- `verifiable-releases`: each release's template and package are also published to a hosted bucket, byte-identical to the release assets; the template still references no bucket of ours.
- `account-coverage`: a new install checks coverage on its first run.
- `guard-jennah-contract`: the heartbeat reports whether SCPs are enabled and which scope targets have no free SCP slot; the guard's two credentials may come from secrets the stack created from an install code.

## Impact

- `.github/workflows/release.yml`: an upload job using an OIDC role in our AWS account, after the assets are signed, refusing to overwrite an existing version.
- `guard/template.yaml`: `InstallCode` parameter, rules making it exclusive with the secret ARNs, two conditional `AWS::SecretsManager::Secret` resources, function environment and IAM pointing at whichever secrets are in use, `organizations:ListRoots` and `organizations:ListPoliciesForTarget` for the guard role, template revision.
- `internal/guard`: first-run coverage (`install.go`, `coverage.go`), the heartbeat's `org` section (`heartbeat.go`), and the SCP checks moved from the CLI's preflight into a shared package (`internal/awsx`) used by both.
- `internal/cli/installlink.go`: an option to link to the hosted files.
- `docs/install-console.md`, `docs/install.md`, `docs/operating.md`, `docs/verify.md`: the install code, the hosted files and how to check them, and the faster first report.
- Coordination: see Dependencies and sequencing.
