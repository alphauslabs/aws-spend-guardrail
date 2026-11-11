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

Today these are API keys with their own scope and dataset selector, minted by a long-lived admin of the enterprise with `jnh` (signed in with `jnh login`; an API key can't mint keys). Once Jennah supports service accounts, use one service account per key instead, each with a custom role carrying the same reach. The guard only needs the secret, so switching doesn't touch the stack.

Mint both keys and store each as a plain-text secret in Secrets Manager in the payer account (us-east-1). Jennah shows a key's secret once, so it goes straight into a file that only you can read, then into Secrets Manager, and the file is deleted:

```
umask 077
jnh keys create --label guardrail-msp_a-123456789012-reader \
  --datasets guardrail.msp_a.123456789012.config \
  --scopes datastore.data:read,datastore.schema:read,approval.requests:create,approval.requests:read,approval.requests:cancel \
  --expires-in 1y -o json | jq -r .secret > reader.key
jnh keys create --label guardrail-msp_a-123456789012-writer \
  --datasets guardrail.msp_a.123456789012.inbox \
  --scopes datastore.data:write \
  --expires-in 1y -o json | jq -r .secret > writer.key

aws secretsmanager create-secret --region us-east-1 --name guardrail/msp_a/reader --secret-string file://reader.key
aws secretsmanager create-secret --region us-east-1 --name guardrail/msp_a/writer --secret-string file://writer.key
rm reader.key writer.key
```

Pass the two secret ARNs to the stack (see [install](install.md)). `jnh keys list` shows each key's expiry and when it was last used.

### Rotate a key before it expires

When a key expires, the guard can't read config or record events: protection already in force keeps working, and the stack alerts that config refresh is failing. Rotate a few days before expiry:

1. Mint a new key with the same label, datasets, and scopes, as above.
2. Store it as the secret's new value: `aws secretsmanager put-secret-value --region us-east-1 --secret-id guardrail/msp_a/reader --secret-string file://reader.key`, then delete the file.
3. The guard reads its secrets when it starts, so it picks up the new key the next time AWS Lambda starts a fresh copy of the function, usually within hours. Once `jnh keys list` shows the new key in use, revoke the old one with `jnh keys revoke <key id>`.

The loosening approvers set in the stack must be members of the Jennah enterprise or on its approver allowlist.

## 3. Change config: pull, edit, publish

```
guardrail pull --msp msp_a --payer 123456789012 -o config.json   # the newest version; status goes to stderr
# edit config.json
guardrail validate config.json                                    # optional; publish validates too
guardrail diff --msp msp_a --payer 123456789012 config.json       # each change marked tightening or loosening
guardrail publish --msp msp_a --payer 123456789012 --note "raise acme dev for the migration" config.json
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

## Accounts nobody protects

Once a day, the guard compares the organization's active accounts with the config. An account that no scope covers (directly, or through a scoped OU) and that isn't in `excluded_accounts` is reported once:

- an `unprotected_account` event in the payer org's inbox in Jennah, with the account's id, name, and parent (root or OU);
- a local alert. The first run after install sends one alert listing all of them.

A scoped account that's no longer an active member of the organization is reported the same way, as `stale_scope`.

To act on a report, publish a new version that either adds the account as a scope (a tightening, so it applies at once) or adds it to `excluded_accounts` (applies at once, no approval). It's reported again only if it lapses later.

New accounts are found at the daily run, 00:15 UTC. With the stack parameter `ReactToAccountEvents=true`, they're found within minutes of being created, invited, or moved, but only if a CloudTrail trail in the payer account records management events. Without a trail, those events never reach EventBridge.

## Alert recipients

Local alerts (freezes, rejected configs, Jennah outages, and unprotected accounts) go to the stack's alert topic. Two kinds of recipient are subscribed to it:

- the stack's `AlertEmail`, set at install. Config can never remove it, so someone still hears about a freeze if Jennah is unreachable or the config is changed by mistake. A distribution list works well here.
- the config's `alert_emails`. Every addition or removal needs the same approval as a loosening.

On each run, the guard compares the topic's email subscriptions with `alert_emails` in the config in force:

- An address with no subscription is subscribed, and AWS sends it a confirmation email. The guard records `alert_recipient_subscribed` in the inbox.
- An address that hasn't confirmed after a day is asked again (one more email), and recorded as `alert_recipient_unconfirmed`. This repeats once a day until it confirms or is removed from the config. AWS drops a subscription that is never confirmed.
- A confirmed subscription that the guard made, for an address no longer listed, is unsubscribed and recorded as `alert_recipient_removed`. An address removed while still unconfirmed can't be unsubscribed yet; if its old link is clicked later, the next run unsubscribes it.

The guard never unsubscribes the stack's `AlertEmail`, a subscription someone added in the SNS console, or an address that was already subscribed before the config listed it. Someone who wants to stop receiving alerts should be removed from the config; unsubscribing from the email only lasts until the next run, which subscribes them again.

## Before installing: check SCP slots

AWS allows 5 SCPs attached directly to one account or OU. A target that already has 5 can't take a freeze policy. With AWS credentials for the payer account:

```
guardrail preflight --msp msp_a config.json
```

It lists each scope's target and fails if any has no free slot.
