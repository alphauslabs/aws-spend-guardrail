# Operating

Day-to-day work with the `guardrail` CLI: set up an MSP in Jennah, mint the guard's credentials, and change config.

Build the CLI with `make guardrail` (it lands in `bin/guardrail`). Every command except `validate`, `schema` and `version` talks to Jennah:

```
export JENNAH_ENDPOINT=<jennah grpc endpoint>
export JENNAH_API_KEY=<key>   # optional; otherwise your jennah CLI login is used
```

## 1. Set up an MSP

Each payer org belongs to one MSP id: lowercase letters, digits, and `_` or `-` inside, for example `msp_a`. Run this once per MSP, with an operator credential that can create datasets and declare tables:

```
guardrail setup --msp msp_a
```

It creates two datasets in your Jennah enterprise, `guardrail.msp_a.config` and `guardrail.msp_a.inbox`, declares their tables, and prints what each guard credential needs. Running it again changes nothing.

People who manage this MSP's config need a role whose dataset selector is `guardrail.msp_a.*`, with `datastore.data:read` and `datastore.data:write`.

## 2. Mint the guard's credentials

The guard uses two keys. Neither can write config or decide an approval, so a leaked key can't loosen protection.

| Key | Dataset selector | Permissions |
|---|---|---|
| reader | `guardrail.msp_a.config` | `datastore.data:read`, `datastore.schema:read`, `approval.requests:create`, `approval.requests:read`, `approval.requests:cancel` |
| writer | `guardrail.msp_a.inbox` | `datastore.data:write` |

Today these are API keys with their own scope and dataset selector, minted by a long-lived admin of the enterprise. Once Jennah supports service accounts, use one service account per key instead, each with a custom role carrying the same reach. The guard only needs the secret, so switching doesn't touch the stack.

Store each key as a plain-text secret in Secrets Manager in the payer account (us-east-1), for example `guardrail/msp_a/reader` and `guardrail/msp_a/writer`. Pass their ARNs to the stack (see [install](install.md)).

The loosening approvers set in the stack must be members of the Jennah enterprise or on its approver allowlist.

## 3. Change config: pull, edit, publish

```
guardrail pull    --msp msp_a -o config.json   # the newest version; status goes to stderr
# edit config.json
guardrail validate config.json                 # optional; publish validates too
guardrail diff    --msp msp_a config.json      # each change marked tightening or loosening
guardrail publish --msp msp_a --note "raise acme dev for the migration" config.json
```

- `pull` writes the newest version, or `--version N`, and tells you on stderr which version the guard last applied and whether the newest one is still pending.
- `diff` compares with the version the guard last applied.
- `publish` refuses an invalid document, shows the classified diff, and writes the next version number. Publishing the same document again writes nothing. If someone else published first, it fails and asks you to pull again.

What happens next is in [config](config.md): tightening applies within about 5 minutes, loosening after approval and the stack's delay.

The format of the document, and every field, is in [config](config.md).

## Before installing: check SCP slots

AWS allows 5 SCPs attached directly to one account or OU. A target that already has 5 can't take a freeze policy. With AWS credentials for the payer account:

```
guardrail preflight --msp msp_a config.json
```

It lists each scope's target and fails if any has no free slot.
