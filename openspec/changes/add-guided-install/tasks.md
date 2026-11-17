# Tasks

## 1. Before starting

- [ ] 1.1 Confirm `add-mantle-usage` and `add-regional-prices` have landed and `add-install-link` is archived (`openspec list` shows none of them in flight, and `openspec/specs/guard-install/spec.md` exists); record the template revision this change takes in design D8
- [ ] 1.2 Get the hosted bucket's name and owning account from a maintainer, and confirm the bucket exists in us-east-1 with public `s3:GetObject` on `guard/*` and no listing; record the name in design D4 and remove it from Open Questions
- [ ] 1.3 In the e2e payer: create and delete a stack holding an `AWS::SecretsManager::Secret` with a fixed name, then create it again; record in design D2 whether the name stays reserved, and drop the stack-id suffix if it doesn't

## 2. Install code in the template

- [ ] 2.1 Add the `InstallCode` parameter (`NoEcho`, `AllowedPattern` from D1), the `UseInstallCode` condition, and `Rules` making it exclusive with the two ARNs (D2); `cfn-lint` passes and the template's parameter groups list it with the Jennah settings
- [ ] 2.2 Add the two conditional secrets, named per D2, and point the function's environment and the role's `GetSecretValue` resource at whichever source is in use; existing template tests pass and a new test asserts both branches
- [ ] 2.3 Bump the template revision (D8)
- [ ] 2.4 e2e: create a stack from an install code and check `config_applied` and a heartbeat reach the inbox; describe the stack and confirm the parameter is masked; update it to an ARN-based stack and back; delete it and confirm both secrets go

## 3. First-run report

- [ ] 3.1 First-run coverage per D5: the install check sets the flag, the applying run checks coverage and clears it; unit test with an empty config and 5 member accounts writes 5 `unprotected_account` events and one alert, and a restart writes none
- [ ] 3.2 SCP readiness per D6: `ListRoots` check plus `awsx.CheckSCPSlots`, run on first run, daily, and on a change of scope targets, kept in state; unit tests for enabled, disabled, a full target, a target already carrying a freeze, and a failed call (`checked: false`)
- [ ] 3.3 Add the `org` section to the heartbeat snapshot; heartbeat tests show a change in readiness writes a heartbeat and an unchanged result doesn't
- [ ] 3.4 Grant the guard role `organizations:ListRoots` and `organizations:ListPoliciesForTarget`; e2e: the first heartbeat after install carries `org` with `scps_enabled: true`

## 4. Hosted release files

- [ ] 4.1 Add the upload job to `.github/workflows/release.yml` per D4: after signing, OIDC role, check assets against `SHA256SUMS`, upload `guard.zip` then `template.yaml` with `If-None-Match: *`; actions pinned by digest (the pinned-actions check passes)
- [ ] 4.2 Add a release gate or tools check that the hosted bucket constant is the one the docs and `install-link` use
- [ ] 4.3 Verify on the next release tag: both files are readable without credentials and their SHA-256 match `SHA256SUMS`; a re-run of the workflow fails at upload and leaves them unchanged

## 5. CLI

- [ ] 5.1 `guardrail install-link --hosted --version V` per D7, refusing `--bucket`/`--prefix` with it and refusing values starting with `jgi1,`; tests pin the URL for a fixed input and cover both refusals

## 6. Docs and release notes

- [ ] 6.1 `docs/install-console.md`: the guided setup in Jennah as the first path, the install code field, and the hosted files; the own-bucket path kept as it is
- [ ] 6.2 `docs/install.md`: `InstallCode` in the parameter table, exclusive with the ARNs, the secrets it creates and their names, and how rotation works for such a stack (D3)
- [ ] 6.3 `docs/verify.md`: checking the hosted files against a release and comparing the running guard's code hash
- [ ] 6.4 `docs/operating.md`: the first report within minutes of install, the heartbeat's `org` section, and `install-link --hosted`
- [ ] 6.5 CHANGELOG entry with its security-relevant section (the install code in a `NoEcho` parameter, the hosted files, the new read-only Organizations permissions) and the template revision's upgrade step; the changelog check passes

## Workflow follow-up

- Archive after the jennah change `add-guardrail-guided-setup` has shipped its install code step, or earlier if the guard side is released first; the guard side does not depend on it.
