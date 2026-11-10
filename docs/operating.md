# Operating

Day-to-day work with the `guardrail` CLI: set up an MSP in Jennah, mint the guard's credentials, and change config.

Build the CLI with `make guardrail` (it lands in `bin/guardrail`). Every command except `validate`, `preflight`, `schema` and `version` talks to Jennah, at its public endpoint unless you set `--endpoint` or `JENNAH_ENDPOINT`. It uses your `jnh` CLI login and its active enterprise, or an API key:

```
export JENNAH_API_KEY=<key>   # optional; otherwise your jnh login is used
```

## 1. Set up a payer org

Each MSP has an id: lowercase letters, digits, and `_` or `-` inside, for example `msp_a`. An MSP can have several payer orgs (AWS organizations). Each payer org has its own guard and its own config, named by the MSP id and the payer (management) account. Run this once per payer org, with an operator credential that can create datasets and declare tables:

```
guardrail setup --msp msp_a --payer 123456789012
```

It creates two datasets in your Jennah enterprise, `guardrail.msp_a.123456789012.config` and `guardrail.msp_a.123456789012.inbox`, declares their tables, and prints what each guard credential needs. Running it again changes nothing.

People who manage config need a role with `datastore.data:read` and `datastore.data:write` and a dataset selector of either:

- `guardrail.msp_a.123456789012.*` for that one payer org, or
- `guardrail.msp_a.*` for every payer org of the MSP.

## 2. Mint the guard's credentials

Each payer org's guard uses two keys of its own. Neither can write config or decide an approval, so a leaked key can't loosen protection, and it reaches only that payer org.

| Key | Dataset selector | Permissions |
|---|---|---|
| reader | `guardrail.msp_a.123456789012.config` | `datastore.data:read`, `datastore.schema:read`, `approval.requests:create`, `approval.requests:read`, `approval.requests:cancel` |
| writer | `guardrail.msp_a.123456789012.inbox` | `datastore.data:write` |

Today these are API keys with their own scope and dataset selector, minted by a long-lived admin of the enterprise. Once Jennah supports service accounts, use one service account per key instead, each with a custom role carrying the same reach. The guard only needs the secret, so switching doesn't touch the stack.

Store each key as a plain-text secret in Secrets Manager in the payer account (us-east-1), for example `guardrail/msp_a/reader` and `guardrail/msp_a/writer`. Pass their ARNs to the stack (see [install](install.md)).

The loosening approvers set in the stack must be members of the Jennah enterprise or on its approver allowlist.

## 3. Change config: pull, edit, publish

```
export P="--msp msp_a --payer 123456789012"
guardrail pull    $P -o config.json   # the newest version; status goes to stderr
# edit config.json
guardrail validate config.json        # optional; publish validates too
guardrail diff    $P config.json      # each change marked tightening or loosening
guardrail publish $P --note "raise acme dev for the migration" config.json
```

- `pull` writes the newest version, or `--version N`, and tells you on stderr which version the guard last applied and whether the newest one is still pending.
- `diff` compares with the version the guard last applied.
- `publish` refuses an invalid document, or one whose `payer_account_id` is not `--payer`, shows the classified diff, and writes the next version number. Publishing the same document again writes nothing. If someone else published first, it fails and asks you to pull again.

What happens next is in [config](config.md): tightening applies within about 5 minutes, loosening after approval and the stack's delay.

The format of the document, and every field, is in [config](config.md).

## Make an install link

Whoever installs the stack in the AWS console can do it from a link that fills in the form. Make it after the release files are in the client's bucket. It needs no AWS or Jennah credentials:

```
guardrail install-link --msp msp_a --payer 123456789012 \
  --bucket guardrail-artifacts-123456789012 --prefix guard/v0.1.0 --version v0.1.0 \
  --reader-secret-arn <reader secret arn> --writer-secret-arn <writer secret arn> \
  --approvers finops-lead@example.com --alert-email oncall@example.com
```

It prints one URL. Options you leave out stay empty in the console form. The link holds no secrets, only ARNs and emails, and the command refuses a Jennah key given as any option. The link sets `ExpectedPayerAccountId`, so it can only create the stack in that payer account. See [install from the console](install-console.md).

## Before installing: check SCP slots

AWS allows 5 SCPs attached directly to one account or OU. A target that already has 5 can't take a freeze policy. With AWS credentials for the payer account:

```
guardrail preflight --msp msp_a config.json
```

It lists each scope's target and fails if any has no free slot.
