# Upgrading

A new release reaches a payer org only when you update its stack. Each payer org has its own stack, so upgrade them one at a time, in a test payer org first if you have one. Guards never check for new releases themselves; the `guardrail` CLI and the Jennah console do.

## 1. Find what each payer org runs

```
guardrail versions --msp msp_a
```

It lists each payer org's running release, code hash, and newest heartbeat, against the latest release on GitHub, with guards that are behind first:

```
latest release v0.2.0
PAYER         VERSION  STATUS   LAST HEARTBEAT        CODE SHA256  NOTE
222222222222  v0.1.0   behind   2026-12-02 00:15 UTC  b3J...=      latest is v0.2.0
111111111111  v0.2.0   current  2026-12-02 00:15 UTC  Zm9...=
```

`silent` means no heartbeat for over 2 days, which needs looking at before any upgrade. `unknown` means no heartbeat yet, an inbox the CLI can't read, or a version that isn't a release. Add `--payer ACCOUNT` for one payer org, `--json` for scripts, and `--latest vX.Y.Z` to compare with a release without asking GitHub (it allows 60 unauthenticated requests an hour per address). It needs only read access to the inboxes.

In the console, **Managed agents → AWS spend guardrail** shows each install's version and marks the ones behind; a payer org's page says which release is available.

## 2. Read what upgrading takes

Read the release's entry in [CHANGELOG.md](../CHANGELOG.md), and the entry of every release between yours and it:

- **Upgrade** lists, in order, what upgrading to that release needs, or says "None." It also says when rolling back to the previous release isn't supported.
- **Security-relevant** lists changes to the freeze policies, IAM permissions, the stack parameters that control approvals, and outbound calls. Review them, and the diff between the two tags, as in [verifying a release](verify.md).

Skipping releases is fine: apply the Upgrade steps of every skipped release in order, then the target's.

## 3. Verify and stage the release

Download `template.yaml` and the function package from the release, and check them as in [verifying a release](verify.md). Upload the package to your bucket under a new key, for example `guard/v0.2.0/guard.zip`, so the previous one stays for a rollback.

## 4. Do the Jennah steps first

If the Upgrade section says so:

- update the `guardrail` CLI to the release;
- run `guardrail setup --msp ID --payer ACCOUNT`, which only adds what the release declares and changes nothing else;
- mint a key with the reach `setup` prints, store it in the secret the stack names, and revoke the old key.

The guard keeps working on the old release meanwhile.

## 5. Update the stack

With the AWS CLI, from the payer account:

```
aws cloudformation deploy \
  --region us-east-1 \
  --stack-name guardrail-msp-a \
  --template-file template.yaml \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides \
    FunctionPackageKey=guard/v0.2.0/guard.zip \
    GuardVersion=v0.2.0
```

Parameters you don't name keep their values. Add any new parameter the Upgrade section asks for.

In the console: **CloudFormation → the stack → Update → Replace existing template**, upload the release's `template.yaml`, change `FunctionPackageKey` and `GuardVersion`, set any new parameter, and submit.

Always update the template and the package together. A guard whose stack template is older than its code needs keeps running, but sends one alert ("stack template is older than this guard needs") and records `template_revision` and `template_revision_needed` in its heartbeat until the template is updated. A `GuardVersion` that doesn't match the running code is alerted once and recorded as `stack_guard_version`; events always report the version built into the code.

## 6. Check it

The guard writes a heartbeat on its first run after its code changes, within 5 minutes. Then:

```
guardrail versions --msp msp_a --payer 123456789012
```

shows the new version as `current`, and its code hash must match the release notes. Freezes, AI cutoffs, and a pending loosening approval carry over unchanged.

## Rolling back

Update the stack back to the previous release's `template.yaml` and package (step 5 with the old values). Rolling back one release is supported unless that release's Upgrade section says otherwise: each release can read the state the next one writes, and the release workflow tests this. Rolling back further, go one release at a time.
