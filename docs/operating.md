# Operating

Day-to-day work with the `guardrail` CLI: set up an MSP in Jennah, mint the guard's credentials, and change config.

Build the CLI with `make guardrail` (it lands in `bin/guardrail`). Every command except `validate`, `preflight`, `schema`, and `version` talks to Jennah, at its public endpoint unless you set `--endpoint` or `JENNAH_ENDPOINT`. It uses your `jnh` CLI login and its active enterprise, or an API key:

```
export JENNAH_API_KEY=<key>   # optional; otherwise your jnh login is used
```

## 1. Set up a payer org

Each MSP has an id: lowercase letters, digits, and `_` or `-` inside, for example `msp_a`. The id `prices` is reserved for the shared prices dataset. An MSP can have several payer orgs (AWS organizations). Each payer org has its own guard and its own config, named by the MSP id and the payer (management) account. Run this once per payer org, with an operator credential that can create datasets and declare tables:

```
guardrail setup --msp msp_a --payer 123456789012
```

It creates two datasets in your Jennah enterprise, `guardrail.msp_a.123456789012.config` and `guardrail.msp_a.123456789012.inbox`, declares their tables, and prints what each guard credential needs. The first `setup` in an enterprise also creates `guardrail.prices`, the AI price table every guard in the enterprise reads (see [shared prices](#shared-ai-prices)); later runs leave it unchanged. Running it again changes nothing, unless a newer release declares something the datasets don't have yet, such as the inbox's retention (see [how long events are kept](#how-long-events-are-kept)).

People who manage config need a role with `datastore.data:read` and `datastore.data:write` and a dataset selector of either:

- `guardrail.msp_a.123456789012.*` for that one payer org, or
- `guardrail.msp_a.*` for every payer org of the MSP.

## 2. Mint the guard's credentials

Each payer org's guard uses two keys of its own. Neither can write config or decide an approval, so a leaked key can't loosen protection, and it reaches only that payer org (the reader also reads the enterprise's shared prices, which can only tighten). The reader reads the payer org's inbox too, so a reinstalled guard can find which config version was in force (see [Reinstall a guard](#reinstall-a-guard)).

| Key | Dataset selector | Permissions |
|---|---|---|
| reader | `guardrail.msp_a.123456789012.config`, `guardrail.prices`, `guardrail.msp_a.123456789012.inbox` | `datastore.data:read`, `datastore.schema:read`, `approval.requests:create`, `approval.requests:read`, `approval.requests:cancel` |
| writer | `guardrail.msp_a.123456789012.inbox` | `datastore.data:write` |

Today these are API keys with their own scope and dataset selector, minted by a long-lived admin of the enterprise with `jnh` (signed in with `jnh login`; an API key can't mint keys). Once Jennah supports service accounts, use one service account per key instead, each with a custom role carrying the same reach. The guard only needs the secret, so switching doesn't touch the stack.

Mint both keys and store each as a plain-text secret in Secrets Manager in the payer account (us-east-1). Jennah shows a key's secret once, so it goes straight into a file that only you can read, then into Secrets Manager, and the file is deleted:

```
umask 077
jnh keys create --label guardrail-msp_a-123456789012-reader \
  --datasets guardrail.msp_a.123456789012.config,guardrail.prices,guardrail.msp_a.123456789012.inbox \
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

When a key expires, the guard can't read config or record events: protection already in force keeps working, and the stack alerts that config refresh is failing. The guard warns you first. Once a day it asks Jennah about each of its keys, using the key itself, and sends a local alert when one expires within 30, 14, 7, 3, or 1 days, once at each step (a guard that missed some days sends one alert for where it is now). The daily `heartbeat` event in the inbox lists both keys under `keys`, with each key's label and `expires_at`: a time, `never` for a key minted without an expiry, or `unknown` when Jennah couldn't say (for example while it's unreachable), which never triggers a warning.

Rotate a few days before expiry:

1. Mint a new key with the same label, datasets, and scopes, as above.
2. Store it as the secret's new value: `aws secretsmanager put-secret-value --region us-east-1 --secret-id guardrail/msp_a/reader --secret-string file://reader.key`, then delete the file.
3. The guard reads its secrets when it starts, so it picks up the new key the next time AWS Lambda starts a fresh copy of the function, usually within hours. Once `jnh keys list` shows the new key in use, revoke the old one with `jnh keys revoke <key id>`.

The loosening approvers set in the stack must be members of the Jennah enterprise or on its approver allowlist.

A reader key minted before the inbox was added to its reach keeps working, except when the guard is reinstalled: then the guard can't check which version was in force, alerts once (`guardrail: reinstall check not done`), and applies the latest version as it is. Rotate the reader with the inbox added, as above, before you reinstall.

### Reinstall a guard

Deleting the stack deletes the guard's state, so a recreated guard starts with none. On its first run it reads the inbox for the last version a guard applied in full, puts that version back in force at once, and handles anything newer as a change from it:

- A newer version's tightening applies at once, and its loosening waits for approval and the loosening delay, as for any version. If its approval was already raised, the guard finds it through the earlier install's `loosening_raised` event and uses that one (its own `loosening_raised` says `adopted`), so approvers aren't asked again. If that event never reached the inbox, the guard raises a new approval, and approvers get a second request for the version. Either way the delay counts from the new guard's first run. An adopted approval still shows the earliest apply time the earlier install wrote, so after a reinstall the change applies later than the approval says (never earlier); Jennah can't edit a sent approval.
- A loosening that was rejected stays rejected.
- When the inbox records no applied version, the guard is a first install and applies the latest version at once.

The first run writes a `guard_installed` event giving the version that was in force before (`previous_applied_version`), the latest version, and its `outcome`: `resumed`, `fresh`, or `not_done` when the check couldn't run. Events about config versions from the new install carry the install in their ID (for example `config_applied:10:i1794536600`), so they're recorded beside the earlier install's events instead of being dropped as duplicates.

The check trusts the inbox: whoever can write it (the guard's writer key, and enterprise admins) could record a looser version as applied, and a later reinstall would put that version in force. That version must still be a published config version.

Deleting the stack still lifts every freeze, by design (see [install](install.md#remove)); only the approval of a pending loosening is kept across a reinstall.

## 3. Change config: pull, edit, publish

```
guardrail pull --msp msp_a --payer 123456789012 -o config.json   # the newest version; status goes to stderr
# edit config.json
guardrail validate config.json                                    # optional; publish validates too
guardrail diff --msp msp_a --payer 123456789012 config.json       # each change marked tightening or loosening
guardrail publish --msp msp_a --payer 123456789012 --note "raise acme dev for the migration" config.json
```

- `pull` writes the newest version, or `--version N`, and tells you on stderr which version the guard last applied and whether the newest one is still pending.
- `diff` compares with what the guard will compare with once it has read the newest published version: the version it last applied; or a newer one that only tightens it, which the guard applies on its next pull even before the inbox reports it; or, while a newer version's loosening waits for approval, the applied version with that version's tightening parts. Its "compared with" line says which.
- `publish` refuses an invalid document, or one whose `payer_account_id` is not `--payer`, shows the classified diff, and writes the next version number. Publishing the same document again writes nothing. If someone else published first, it fails and asks you to pull again. For a version that loosens anything, `--note` is the reason approvers see in the approval request (up to 4,000 characters shown), so say why.

What happens next is in [config](config.md): tightening applies within about 5 minutes, loosening after approval and the stack's delay.

A loosening reaches each approver as a Jennah approval. It names the kind of change, lists each change as a sentence, says what approving would do to any frozen scope it touches (with the scope's Cost Explorer spend), and gives the exact times. Accounts in the changes and freeze lines are named the way alerts name them (see [Alert recipients](#alert-recipients)); the payer id, the reason given, and the approval's JSON payload keep bare ids. For example, raising a frozen account named `client-a` above its spend:

```
Raise a spend cap for payer 123456789012 (config version 2)

Config version 2 for payer account 123456789012 (MSP msp_a) loosens protection. If approved, it makes this change:

- Raise the ceiling of account 111122223333 (client-a) from 5.30 USD to 7.00 USD.

Account 111122223333 (client-a) is frozen, and its spend this month is 6.16 USD, so this change would lift the freeze.

Reason given:
Month-end batch, on trend for 6 months.

If you approve, the change applies 30 minutes after the guard first saw this version, at 09:30 UTC on 2026-11-10 at the earliest. If you reject it, or nobody answers by 11:00 UTC on 2026-11-10, the stricter config stays in force.
```

The approval's payload keeps the change lines `guardrail diff` prints, and adds `earliest_apply_at` and `expires_at`.

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

## When Budgets reports no spend

AWS Budgets resets a budget's spend to zero whenever the budget is created or updated, and can take about a day to recalculate it. Until then the budget's freeze action can't fire. The guard covers that window with Cost Explorer:

- **After it changes a budget** (a new scope, a new ceiling or threshold, or a mode change), the guard reads the scope's cost in Cost Explorer on the same run, then about once an hour. If the cost is already at or over the threshold, a scope in `enforce` is frozen by the guard attaching the freeze policy itself, and a scope in `dry-run` gets `would_freeze`. It stops once the budget reports spend again, after 48 hours, or at a new budget period. From then on the Budgets action freezes on its own, as before.
- **When a raised ceiling could lift a freeze**, the guard compares the new threshold with Cost Explorer's figure for the period, not the budget's. A raise that is still below spend keeps the freeze on every run. If Cost Explorer can't be read, the freeze stays, and an alert says so once until a read succeeds.

**A config change to a frozen scope keeps it frozen.** When AWS Budgets' action froze the scope and the guard then updates its budget (any change of ceiling, threshold, or mode), AWS resets the action, and the reset detaches the freeze policy within a second. The guard waits for the reset to finish, up to 30 seconds, and attaches the policy again, unless the change lifts the freeze. It records this as `freeze_carried`, not as a new freeze, so no second freeze alert is sent. If the reset takes longer than the run waits, the next run, about 5 minutes later, attaches the policy.

Freeze, `would_freeze`, and lift events record `spend_source` (`budget` or `cost_explorer`), and freeze events record `how` the policy was attached (`budget_action` or `guard_attach`). Cost Explorer data trails usage by a few hours, so a freeze from the watch can come a few hours after the spend that caused it.

## Alert recipients

Local alerts (freezes, rejected configs, Jennah outages, and unprotected accounts) go to the stack's alert topic. Two kinds of recipient are subscribed to it:

- the stack's `AlertEmail`, set at install. Config can never remove it, so someone still hears about a freeze if Jennah is unreachable or the config is changed by mistake. A distribution list works well here.
- the config's `alert_emails`. Every addition or removal needs the same approval as a loosening.

Alerts name the accounts they mention, for example `Account 730335604511 (e2enormal) is cut off`, with names from the organization's account list (read at most once an hour, with the permission the guard already has). Account ids inside ARNs, cutoff ids, and the `Error:` line are left as they are. When names can't be read, the alert goes out at once with bare ids; when a name would make the subject longer than the 100 characters SNS allows, the subject keeps the bare id and the body still names it. Loosening approvals name accounts the same way. Events in the inbox keep identifying accounts by id only.

On each run, the guard compares the topic's email subscriptions with `alert_emails` in the config in force:

- An address with no subscription is subscribed, and AWS sends it a confirmation email. The guard records `alert_recipient_subscribed` in the inbox.
- An address that hasn't confirmed after a day is asked again (one more email), and recorded as `alert_recipient_unconfirmed`. This repeats once a day until it confirms or is removed from the config. AWS drops a subscription that is never confirmed.
- A confirmed subscription that the guard made, for an address no longer listed, is unsubscribed and recorded as `alert_recipient_removed`. An address removed while still unconfirmed can't be unsubscribed yet; if its old link is clicked later, the next run unsubscribes it. If AWS refuses the unsubscribe, the guard sends one local alert (`guardrail: alert recipient not removed`) saying the address still receives alerts, and tries again on every run.

The guard never unsubscribes the stack's `AlertEmail`, a subscription someone added in the SNS console, or an address that was already subscribed before the config listed it. Someone who wants to stop receiving alerts should be removed from the config; unsubscribing from the email only lasts until the next run, which subscribes them again.

## The guard's state

The newest `heartbeat` event in the inbox is the guard's state as it last recorded it, so you never need to piece it together from other events:

- `scopes`: every scope in force, with its `target`, `mode`, `ceiling_usd`, `threshold_pct`, `period`, `freeze_style`, and `actual_usd` as Budgets last reported it. A frozen scope has `frozen` with `since`, `how`, and `policy_id`. A freeze still in place for a scope no longer in the config is listed with `in_config: false`.
- `pending`: the loosening waiting for approval, with its `version`, `approval_id`, and `earliest_apply_at`.
- `excluded` and `unprotected`: the accounts left out on purpose, and those reported as covered by no scope.
- `ai`, with the AI guard on: its `mode`, the accounts in `cutoffs`, whether the `trail` is stalled, and `unknown_models`. With a block policy, `blocked` gives the `accounts` and kinds it denies (as the guard last wrote it), whether it is `attached`, and any `failing` write it is retrying. With mantle regions watched, `mantle` gives each region in `regions` with whether its usage can be `read` (and the `error` when not), and the accounts reported as `unlinked`, per region.
- `account_names`: id to name for the accounts the state above mentions, as the alerts name them (listed at most an hour before). It is left out when the guard can't list the organization's accounts, and an account with no name is left out of it. A rename alone doesn't write a heartbeat; the next one carries the new name.
- `applied_version`, `effective_version`, `keys`, `guard_version`, and `code_sha256`, as before, and `state_layout`, which changes when this shape does.

The guard writes a heartbeat once a day (`reason: "daily"`, id `heartbeat:<date>`), and also at the end of any run that changed its state: a freeze or lift, a cutoff or its lift, a version applied, a loosening raised or closed, the trail stalling or resuming, a change in the unprotected accounts, a change in what the block policy denies, or a mantle region becoming unreadable or readable again (`reason: "change"`). Spend moving alone writes nothing. To read the current state:

```
jnh datasets query guardrail.msp_a.123456789012.inbox --table events \
  --where kind=heartbeat --order-by occurred_at:desc --limit 1 -o json
```

Older heartbeats stay for 400 days, so they also answer what the state was at a given time.

## How long events are kept

Jennah removes inbox events some time after they are **400 days** old, judged by `occurred_at`, typically within three more days. 400 days is a lower bound: an event is never removed sooner, and an expired one can still show up in a query for a few days. That keeps a full year of history plus a month for annual reviews. Config versions are kept forever, since they're the record of what was enforced.

Inboxes set up before this retention existed get it the next time you run `guardrail setup` for that payer org with this release. Setup says so first, because it removes events already older than 400 days:

```
guardrail.msp_a.123456789012.inbox: events: rows whose occurred_at is older than 400 days will be removed
```

To keep older events elsewhere, export them before running setup, or at any time before they turn 400 days old:

```
jnh datasets query guardrail.msp_a.123456789012.inbox --table events \
  --where 'occurred_at<2026-01-01T00:00:00Z' --order-by occurred_at:asc --limit 1000 -o ndjson > events.ndjson
```

Each line is one event, oldest first. A typical inbox holds a few events a day, so 1,000 lines cover years. If you get exactly 1,000, run it again with a second `--where 'occurred_at>=<the last line's occurred_at>'` and append, dropping the one repeated line.

## AI guard

Turning it on, its caps and rules, and lifting a cutoff are in [AI guard](ai-guard.md). In short: check the trail with `guardrail preflight --ai`, update the stack with `EnableAIGuard=true`, publish an `ai` section in dry-run, then enforce. A cutoff's alert gives its id; lift it by adding the id to `ai.lifts` in a new version, with a reason in `--note`.

## Shared AI prices

The AI guard prices Bedrock calls from the table in its release, merged with the enterprise's shared table in `guardrail.prices`, which every guard reads on its 5-minute pull. Publish it when AWS changes prices, once per enterprise (not per MSP or payer org), with an operator credential that can write that dataset and the AWS credentials of any account that can call the Price List API (`pricing:GetProducts` and `pricing:GetAttributeValues`), `bedrock:ListFoundationModels`, and `bedrock-mantle:ListModels`:

```
guardrail prices publish --dry-run
guardrail prices publish --note "Nova price change, Dec 2026"
```

The build reads the Price List for every region, and Bedrock's and the `bedrock-mantle` endpoint's model lists in every region your account can reach, which takes under a minute. It prices each model in each region, and names the regions whose model lists it couldn't read, usually those your account hasn't enabled; those regions are still priced, for the models listed elsewhere. A version holds one chunk of prices per region. Guards before regional prices read only the us-east-1 chunk, which also lists each model not offered in us-east-1 at its highest price, so they keep working, at us-east-1 prices, until upgraded.

It prints the enterprise it acts on and the models added, raised, lowered, and removed against the latest version, per model across regions ("raised (in 12 regions, largest in eu-west-2)"), then writes a new version (nothing, if nothing changed). The first version with prices per region compares each region with the us-east-1 prices guards used everywhere until then, so it lists most models as raised in some regions. Versions are never edited. Shared prices only tighten: a guard takes the higher of its release's price and the shared one, so a lowered or removed price changes nothing until a release with the lower price is installed, and a payer org's own `ai.prices` still wins. Each guard records the version it starts using as a `prices_applied` event, and every AI cutoff names the price version behind it. A guard that can't read shared prices alerts once and keeps the last version it read, or its release's prices.

### Manual prices

The build prices only the models it can match between the Price List and Bedrock's model list, and lists the rest as "no price found". To price one of those for every guard in the enterprise, give its price in a file:

```
{"models": {"xai.grok-4.6": {"input_per_1k": 0.003, "output_per_1k": 0.015}}}
```

```
guardrail prices publish --overrides manual.json --note "Grok 4.6 from AWS's pricing page, Nov 2026"
```

Prices are USD per 1,000 input and output tokens, both above zero. Model ids are read like the AI guard reads them, so `us.xai.grok-4.6` and `xai.grok-4.6` are one model. The file is refused, and nothing published, if it has any other field.

- A manual price applies in every region. A model the Price List doesn't price in a region is published there at the manual price. Where it does, each of input and output is published at the higher of the two, and the command says where the Price List was higher.
- Manual prices are kept in the published version and carried into later versions, so a publish without `--overrides` keeps them, and a file only needs the models you add or change.
- When the Price List starts pricing a manual model at or above its manual price on both input and output in every region Bedrock offers it in, the next publish drops the manual price by itself, since that changes no price guards use, and says so in the version's note; no `--note` is needed. When the manual price is still higher on either side, it is kept and the command suggests dropping it.
- `--drop-override MODEL` (repeatable) removes a manual price.
- Adding, changing, or dropping a manual price needs `--note`, so each one has a reason in the version history. A publish that only carries them forward needs none.
- For a model its release doesn't list, a guard charges at least the release's highest price for that model's provider (or for any model, if the provider has none in the release). The command warns when a manual price is below that floor, since guards will charge the floor instead. The floor keeps a shared price from ever lowering what a guard charges, so it can't be turned off; to charge the actual price, approve it in each payer org's config (step 5 below).

Shared prices still only tighten: a manual price can add a model or raise a price, never lower what a guard charges.

To price a model the Price List doesn't have yet, step by step:

1. Find the models: `guardrail prices publish --dry-run` (or `guardrail prices build -o FILE`) lists each as "no price found for <id> (<name>)", and a guard that publishes on a schedule alerts on new ones. Only models priced per token need a manual price; rerank models (priced per query) and video models such as Pegasus (priced per second) can't be counted by tokens. A model id that `aws bedrock get-foundation-model --model-identifier <id>` doesn't find in your regions needs nothing until it is offered there.
2. Find its price on AWS: the model's card in the Bedrock user guide (its Pricing section) or the [Bedrock pricing page](https://aws.amazon.com/bedrock/pricing/). Take the Standard tier, short-context row, and of In-Region, Geo CRIS, and Global CRIS, the highest: Global CRIS is usually about 10% cheaper, and a manual price applies to every call. Other tiers (Flex, Priority, Ultrafast), long-context rates, and cache prices aren't used; the Price List build leaves them out too.
3. Convert USD per 1 million tokens to USD per 1,000 by dividing by 1,000: $8.80 input and $44.00 output become `0.0088` and `0.044`.
4. Write the file, run `guardrail prices publish --overrides manual.json --dry-run`, and check the added lines and any floor warning. Then publish with `--note` naming the source and date, for example `--note "GPT-6.2 Astra and Sol from their Bedrock model cards, Nov 2026"`.
5. If publishing warned that guards will charge the floor, they overestimate the model until a release lists it, so caps trip early. For a payer org that uses the model and needs the actual price sooner, add the same price to its config's `ai.prices`, for example `"prices": {"openai.gpt-6.2-sol": {"input_per_1k": 0.0022, "output_per_1k": 0.011}}`, and publish it. A new or lower `ai.prices` entry is a loosening, so it applies after the org's approval and delay; once applied, that org's guard charges it in every region, over the release, shared, and floor prices. Remove the entry once a release you've installed lists the model, so the org follows the Price List again (removing an entry also needs approval).
6. Nothing to do later for the shared price: once the Price List prices the model at or above the manual price everywhere, a publish drops the manual price by itself. If it lands lower, the publish (and the scheduled guard's alert) suggests `--drop-override`.

### Publishing on a schedule

Instead of running `prices publish` by hand, one guard per enterprise can publish every day at 00:45 UTC. It does what `guardrail prices publish` does with no `--overrides`: builds from the Price List, carries manual prices forward, and writes a version only when something changed, with the note "scheduled publish by guard <payer id>". To turn it on, in one payer org of the enterprise:

1. Run `guardrail setup` for that payer org. It prints the price publisher credential's reach: `guardrail.prices`, with `datastore.data:read` and `datastore.data:write`.
2. Mint that key and store it in Secrets Manager in the payer account:

   ```
   umask 077
   jnh keys create --label guardrail-prices-publisher --datasets guardrail.prices \
     --scopes datastore.data:read,datastore.data:write --expires-in 1y -o json | jq -r .secret > publisher.key
   aws secretsmanager create-secret --region us-east-1 --name guardrail/prices/publisher --secret-string file://publisher.key
   rm publisher.key
   ```

3. Update the stack with `PublishSharedPrices=true` and `PricePublisherSecretArn` set to that secret's ARN.

The guard reads the key only when it publishes and when it checks key expiry; its expiry is warned like the reader and writer keys. Its role gains `pricing:GetProducts`, `pricing:GetAttributeValues`, `bedrock:ListFoundationModels`, and `bedrock-mantle:ListModels` on the account's own mantle projects, which read public data, and read access to that one secret. It sends local alerts:

- once when a scheduled publish fails, and once when it works again;
- when it publishes a version, listing what changed;
- when the Price List has no price, in any region, for a model it didn't report before, so you can add a manual price;
- when a manual price is above the Price List's on input or output, so you can drop it if the Price List is right.

Turn it on in only one payer org; a second publishing guard is harmless, since a publish with nothing changed writes nothing, but it doubles the alerts. Before deleting that payer org's stack, turn it on in another, or publishing stops.

`guardrail prices build -o internal/packs/ai/prices.json` regenerates the release's table the same way.

## Before installing: check SCP slots and budget actions

AWS allows 10 SCPs attached directly to one account or OU. A target that already has 10, none of them a freeze policy, can't take one. With AWS credentials for the payer account:

```
guardrail preflight --msp msp_a config.json
```

It lists each scope's target and fails if any has no free slot.

It also checks room for budget actions. Each enforcing scope needs one in the payer account, and AWS allows 100 per account, including budget actions you made outside the guard. Preflight prints a line like:

```
budget actions: 3 outside the guard, 40 enforcing scopes in this config, limit 100
```

and fails when the two add up to more than 100. Reading them needs `budgets:DescribeBudgetActionsForAccount` in the payer account.

## When AWS refuses a budget action

If the payer account reaches AWS's limit of 100 budget actions anyway (for example, budget actions made outside the guard after install, or a loosening that swaps enforcing scopes while it waits for approval, when the guard keeps both sets), AWS refuses the next scope's action. The guard still writes that scope's budget, but the scope can't freeze until it has an action. You get one alert, "guardrail: scopes can't freeze, budget action limit reached", naming the scopes. The guard tries again on every run, and creates the action as soon as there is room. To make room, enforce fewer scopes (OU scopes, or `dry-run`), or remove budget actions outside the guard.

## Lift a freeze or cutoff by hand

Normally a freeze or AI cutoff is lifted through config: a version that raises the ceiling or cap, or lists the cutoff under `ai.lifts`, approved and past the stack's delay. Sometimes that isn't possible in time: Jennah is unreachable for a long time, the approvers can't be reached, or the Jennah plan lapsed, so no new config version can be published (the guard keeps reading and enforcing the config it has, but can't receive changes or record events). Then someone with administrator access to the payer account can detach the policy directly. Each step below is recorded by CloudTrail in the payer account.

The freeze policy ids are in the stack's outputs (`FreezePolicyAId`, `FreezePolicyBId`). Find the AI policies by name, and the organization root with `aws organizations list-roots`:

```
aws organizations list-policies --filter SERVICE_CONTROL_POLICY \
  --query "Policies[?starts_with(Name, 'guardrail-')].[Name,Id]" --output text
```

- **A budget freeze:** detach the freeze policy from the account or OU:

  ```
  aws organizations detach-policy --policy-id <freeze policy id> --target-id <account id or ou id>
  ```

  At its next daily run the guard reports it as drift ("freeze policy detached outside the guard"), forgets the freeze, and doesn't attach it again. The budget's action has already run, so AWS Budgets doesn't apply it again either. The account is unfrozen until a later config change or a new budget period makes the budget fire again.

- **An AI cutoff:** detach the AI cutoff policy (`guardrail-ai-cutoff-<msp id>`) from the account:

  ```
  aws organizations detach-policy --policy-id <cutoff policy id> --target-id <account id>
  ```

  Within a few minutes the guard notices, counts the cutoff as lifted ("detached outside the guard"), with a `cutoff_lifted` and a `drift` event and one alert, and watches the account again: a new breach cuts it off again with a new id. If the account must keep working beyond its cap until a config change can be published, also turn the AI guard off (`EnableAIGuard=false`). Guards before v0.2.8 don't notice a detached cutoff: they keep the account recorded as cut off, never cut it off again, and leave its AI use uncapped until the cutoff is lifted through config.

  The daily run also reports the AI cutoff policy attached to an account the guard has no cutoff for, and leaves it attached.

- **The AI block policy** (`guardrail-ai-blocked-<msp id>`, see [AI guard](ai-guard.md#blocking-what-the-guard-cant-count)): the guard's daily run attaches it again if it is detached by hand. To stop blocking without a config change, detach it from the organization root and then update the stack with `EnableAIGuard=false`, which removes the policy and turns the AI guard off:

  ```
  aws organizations detach-policy --policy-id <block policy id> --target-id <root id>
  ```

After any of these, tell the approvers why, since the change bypassed them, and bring the config in line once you can publish again.
