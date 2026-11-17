# Design

## Context

- The template takes its keys as two secret ARNs (`ReaderSecretArn`, `WriterSecretArn`). They feed the function's environment (`READER_SECRET_ARN`, `WRITER_SECRET_ARN`) and the role's `secretsmanager:GetSecretValue` statement.
- The template takes its package as `FunctionPackageBucket` and `FunctionPackageKey`, and `verifiable-releases` forbids it from naming any bucket of ours. Releases are built by public CI from a tag, reproducible, and Sigstore-signed. The guard reports its code hash in every heartbeat.
- `add-install-link` (4 of 5 tasks done, not archived) introduces `guard-install`, `ExpectedPayerAccountId`, and `guardrail install-link`. It lists hosting our own files as a non-goal.
- Coverage (`internal/guard/coverage.go`) runs at the daily run and on account events, and only once a config is applied. The SCP slot check exists as `awsx.CheckSCPSlots`, used by `guardrail preflight`. Nothing checks whether SCPs are enabled.
- The heartbeat (`internal/guard/heartbeat.go`) carries `state_layout` 1. A heartbeat is written daily and whenever the snapshot's digest changes.
- Jennah API keys are `jennah_sk_` followed by base64url (no commas).
- `add-mantle-usage` and `add-regional-prices` are in flight and edit the template and `internal/guard`. `add-regional-prices` claims template revision 4.

## Goals / Non-Goals

**Goals:**
- A person with no terminal can install the guard from Jennah's guided setup. The setup provides the install code, and the link points at the hosted files.
- Existing installs, pipelines, and the own-bucket path keep working unchanged.
- The guided setup can confirm its steps from the inbox within minutes of the stack being created.

**Non-Goals:**
- Enrollment without a pasted code. The stack would prove its account to Jennah (a presigned `sts:GetCallerIdentity`) and fetch its keys. This is a later change, and the guide is shaped so it can replace the paste step.
- The AI guard, a trail, or price publishing in the guided path.
- Turning SCPs on from the stack.
- Building the hosted bucket's infrastructure from this repository. The bucket and its OIDC role are set up once, by hand or in our infra repo, and the workflow only uploads.

## Decisions

### D1: Install code format `jgi1,<reader>,<writer>`

- One value means one paste.
- The `jgi1` prefix versions the format and makes a code recognizable, so `install-link` and docs can refuse one where it doesn't belong.
- A comma can't occur in a Jennah key, and CloudFormation can split on it with `!Split` and `!Select`. So no custom resource is needed to unpack it.
- `AllowedPattern` on the parameter, `^(jgi1,jennah_sk_[A-Za-z0-9_-]+,jennah_sk_[A-Za-z0-9_-]+)?$`, refuses a malformed code before anything is created.

Alternatives considered:
- Base64 JSON: needs a custom resource to decode, and gives a worse error when it's malformed.
- Two `NoEcho` parameters: that's two pastes, and the setup step gets harder to explain.

### D2: The stack creates the secrets, unique names under `guardrail/<MSPId>/`

There are two `AWS::SecretsManager::Secret` resources, under the condition `UseInstallCode`, each holding one split value.

- **Names.** They are named `guardrail/<MSPId>/reader-<suffix>` and `writer-<suffix>`, with the suffix taken from the stack id. A secret deleted with its stack can keep its name reserved through a recovery window, and fixed names would then block a reinstall. Keeping the `guardrail/<MSPId>/` prefix keeps the payer-side protection policy in `docs/install.md` (`secret:guardrail/msp_a/*`) working.
- **Choosing the source.** The function's environment and the role's `GetSecretValue` resource use `!If [UseInstallCode, !Ref ReaderSecret, !Ref ReaderSecretArn]`.
- **Exclusivity.** `Rules` enforce it. With an install code, both ARNs must be empty. Without one, both ARNs must be set.
- **Verify early.** Whether CloudFormation's delete leaves a name reserved should be checked in the first task, because it decides whether the suffix is needed.

### D3: A stack update keeps the install code's secrets

Updating a stack with "use previous value" leaves `InstallCode` unchanged, so the secrets aren't touched. Key rotation for an install-code stack is the same as today: `put-secret-value` on the stack's secret. The docs warn that passing a new `InstallCode` in a stack update replaces both values. That is also an acceptable way to rotate, because it's a stack update in the payer account.

Moving an install from ARNs to an install code, or back, is allowed, but the docs don't promote it.

### D4: Hosted bucket, uploaded by the release workflow after signing

- **Job.** A new job runs after the signing job. It assumes a role in our AWS account through GitHub OIDC. The role is trusted only for this repository's `release.yml` on `refs/tags/v*`, and may only `s3:PutObject` under `guard/*` in the hosted bucket.
- **Checks before upload.** The job checks the downloaded assets against the signed `SHA256SUMS`.
- **Upload order.** It uploads `guard.zip` first and `template.yaml` last, each with `If-None-Match: *` so an existing object is never replaced. A template is then never present without its package. A failed run leaves at most a package with no template, which a re-run refuses. A maintainer removes that by hand before tagging a new patch release.
- **Bucket.** The bucket is in us-east-1, because Lambda needs the package in the function's region. Its policy grants `s3:GetObject` on `guard/*` to everyone, and allows listing nothing.
- **Bucket name.** Open question. The name goes in one constant shared by `install-link` and the docs, and the jennah guide takes it from its own config.

Alternative considered: a mirror service outside the release workflow. That would break "every asset is built by that workflow run" in spirit, and add an unsigned step.

### D5: First-run coverage hooks into the install check

`checkInstall` already knows when a run is a new install's first. That run sets a flag in the state. The run that applies a version, which is the same run in a fresh install, calls `coverage` when the flag is set and then clears it. A restart with state intact never sets the flag.

An empty `scopes` list already resolves to an effective config, so coverage has something to compare against. The existing once-only reporting means the next daily run reports nothing new.

### D6: SCP readiness in the heartbeat's `org` section

The `org` section looks like this:

```
"org": {
  "checked": true,
  "scps_enabled": true,
  "full_targets": [ { "target": "ou-ab12-cd34ef56", "attached": 10 } ]
}
```

When a call fails, the section is `{"checked": false, "error": "<short reason>"}`.

- `scps_enabled` comes from `organizations:ListRoots`: the root's policy types include `SERVICE_CONTROL_POLICY` with status `ENABLED`.
- `full_targets` reuses `awsx.CheckSCPSlots`, with the stack's two freeze policy names, so a target already carrying a freeze is not counted as full.
- The section is computed on the first run, at the daily run, and when a version changes the set of scope targets. The result is kept in the state, so the snapshot (and its digest) changes only when the result changes. This avoids Organizations calls on every 5-minute run.
- The section is an addition, so `state_layout` stays 1, as the heartbeat spec allows for added fields.
- The role gains `organizations:ListRoots` and `organizations:ListPoliciesForTarget`, both read-only.

The guided setup reads `scps_enabled` to mark its SCP step verified, and `full_targets` to warn before a cap is set on a full target.

### D7: `install-link --hosted`

`--hosted` replaces `--bucket` and `--prefix`, and requires `--version`. It sets the template URL and the package parameters to the hosted bucket. The refusal of key-like values is extended to `jgi1,`. The jennah console builds the same URL itself, and the parameter names and stack name rule are the shared contract. A test pins the URL `--hosted` produces for a fixed input, so the console can copy it.

### D8: Ordering against changes in flight

This change is implemented after `add-mantle-usage` and `add-regional-prices` land, and after `add-install-link` is archived, so that `guard-install` exists as a main spec. It takes the next free template revision at that time. The tasks start with that check.

## Risks / Trade-offs

- **The install code passes through the clipboard and a `NoEcho` parameter.**
  - CloudFormation masks it in describe calls and the console, but it is stored with the stack, and anyone who can see the clipboard or the browser at that moment sees two live keys.
  - Mitigations: each key reaches only one payer org's datasets, neither can loosen protection, and the guide can revoke an unused pair.
  - Enrollment (a non-goal here) removes this.
- **Hosted files make us a supply-chain point for clients who use them.**
  - Mitigations: files are byte-identical to signed assets and never replaced; the guard reports its code hash; the console can flag a hash that matches no release; the own-bucket path remains.
  - A compromise of the upload role could add a new version path, but couldn't change an existing one.
- **Reversing a stated non-goal of add-install-link.** This is recorded in the proposal so the decision is visible at archive. The `verifiable-releases` requirement that the template names no bucket of ours is kept.
- **Organizations rate limits.** The checks run a handful of times a day, plus one call per scope target. An organization with 100 targets makes about 100 `ListPoliciesForTarget` calls in those runs. That is well within limits, and spread across the run like `preflight`'s.
- **Merge friction** with the two changes in flight, mitigated by D8.

## Open Questions

- The hosted bucket's name and owning AWS account.
- Whether a stack-deleted secret's name stays reserved (D2). If not, the suffix can go.
