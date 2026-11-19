# Changelog

Every release has an entry here. Each entry has an **Upgrade** section that lists, in order, what upgrading to it from the previous release needs (updating the CLI, rerunning `guardrail setup`, changing a key's reach, setting a stack parameter, updating the stack), says when rolling back to the previous release isn't supported, or says "None." Each entry also has a **Security-relevant** section that lists changes to the freeze policies, the IAM permissions the stack creates, the stack parameters that control approvals, and the guard's outbound network calls, or says "None." A release without both is refused by the release workflow. See [upgrading](docs/upgrade.md).

## [Unreleased]

## [v0.2.9] - 2026-11-19

### Upgrade
1. Update the stack with this release's package, as in [upgrading](docs/upgrade.md). The template is unchanged (revision 7).

Rolling back to v0.2.8 is supported; its heartbeats leave out the new fields.

### Added
- The heartbeat's `ai` section carries what the AI block policy denies (accounts and kinds, whether it is attached, and any failing write), each watched mantle region with whether its usage can be read and the accounts not linked in it, and `unknown_models`. A change to any of them writes a heartbeat. The layout version stays 1. See [the guard's state](docs/operating.md#the-guards-state).

### Security-relevant
None.

## [v0.2.8] - 2026-11-18

### Upgrade
1. Update the stack with this release's template and package (template revision 7), as in [upgrading](docs/upgrade.md). A guard of this release on an older template keeps running but alerts that its template is older than it needs.
2. Update the `guardrail` CLI.

Rolling back to v0.2.7 is supported.

### Fixed
- An AI cutoff policy detached by hand (an emergency lift, see [lift a freeze or cutoff by hand](docs/operating.md#lift-a-freeze-or-cutoff-by-hand)) is noticed within minutes: the guard counts the cutoff as lifted, records `cutoff_lifted` and `drift` events, alerts once, and watches the account again. Before, the guard kept the account recorded as cut off, never cut it off again, and left its AI use uncapped until the cutoff was lifted through config. The daily run also reports the AI cutoff policy attached to an account the guard has no cutoff for.
- `guardrail diff` and `publish` compare a document with what the guard will compare it with, instead of the version the inbox last reported applied. Right after a tightening version was applied, before its report reached the inbox, they compared with the version before it, so undoing part of it showed as a tightening while the guard treated it as a loosening needing approval. While a loosening waits, they now compare with the applied version plus the waiting version's tightening parts, as the guard does. The "compared with" line says which.

### Security-relevant
- With the AI guard on, the guard's role gains `organizations:ListTargetsForPolicy` on the AI cutoff policy, read once a minute only while the guard holds a cutoff. The template revision is now 7.

## [v0.2.7] - 2026-11-18

### Upgrade
1. Update the stack with this release's template and package (template revision 6), as in [upgrading](docs/upgrade.md). A guard of this release on an older template keeps running but alerts that its template is older than it needs.
2. Optional: block what the AI guard can't count with `blocked` in `ai.defaults` or an AI scope (see [blocking](docs/ai-guard.md#blocking-what-the-guard-cant-count)).

Rolling back to v0.2.6 is supported once nothing is blocked: allow everything again and let the guard detach the block policy first, since the older template can't remove an attached policy.

### Added
- `blocked` in `ai.defaults` and each AI scope: `agents`, `knowledge_bases`, `bidirectional_streams`, and `async_invoke`, the Bedrock use the AI guard can't count, denied in the scope's accounts while it is in enforce mode. A scope's list replaces the defaults'. Blocking applies at once; allowing is a loosening and needs approval. The guard keeps one service control policy listing the blocked accounts per kind, attached to the organization root only while anything is blocked. See `docs/ai-guard.md`.
- `ai_blocked` events; `guardrail diff` notes a dry-run scope that sets `blocked`; `guardrail preflight` shows the root's free SCP slots when the config blocks anything.

### Security-relevant
- A new SCP, `guardrail-ai-blocked-<msp id>`, created by the stack denying nothing, which the guard rewrites to deny the blocked kinds for the listed accounts only, and attaches to the organization root while anything is blocked. Every deny is limited to listed accounts; SCPs never apply to the management account.
- With the AI guard on, the guard's role gains `organizations:UpdatePolicy`, `DescribePolicy`, and `ListTargetsForPolicy` on that policy, `organizations:AttachPolicy` and `DetachPolicy` on it and the organization root, and `organizations:ListRoots`. The template revision is now 6.

## [v0.2.6] - 2026-11-18

### Upgrade
1. Update the stack with this release's template and package (template revision 5), as in [upgrading](docs/upgrade.md). A guard of this release on an older template keeps running but alerts that its template is older than it needs.
2. Optional, to count `bedrock-mantle` calls: in each region you use mantle in, create a CloudWatch observability sink in the payer account and link the member accounts to it, then set `MantleRegions` (see [mantle calls](docs/ai-guard.md#mantle-calls)). Without it, mantle calls are not counted, as before.

Rolling back to v0.2.5 is supported; it stops counting mantle calls, and the sink and links go unused.

### Added
- The AI guard counts calls through the `bedrock-mantle` endpoint (OpenAI Chat Completions and Responses, and Anthropic Messages), which CloudTrail records without token counts. It reads each linked account's mantle inferences and tokens per model and minute from CloudWatch, through a cross-account observability sink in the payer account, once a minute in each region in the new `MantleRegions` stack parameter, and prices and caps them like other calls, at the region's price. A mantle cutoff typically lands within 2 minutes. See `docs/ai-guard.md`.
- An alert when a listed region's mantle usage can't be read, and when it can again; a daily `mantle_unlinked` event and alert, once per account, for active accounts not linked in a listed region.
- `guardrail preflight --ai --mantle-regions` shows each region's sink and the accounts not linked yet.

### Security-relevant
- With the AI guard on, the guard's role gains `cloudwatch:GetMetricData`, `cloudwatch:ListMetrics`, and `oam:ListSinks` (no resource-level permissions), and `oam:ListAttachedLinks` on the account's own sinks. It reads only metrics that member accounts chose to share through the sink.
- New stack parameter `MantleRegions`. The template revision is now 5.

## [v0.2.5] - 2026-11-18

### Upgrade
1. Update the stack with this release's template and package (template revision 4), as in [upgrading](docs/upgrade.md). A guard of this release on an older template keeps running but alerts that its template is older than it needs.
2. If a payer org publishes shared prices on a schedule (`PublishSharedPrices=true`), the update gives its role the new read permissions; its next daily publish writes the first version with prices per region. To publish by hand, update the `guardrail` CLI and give its AWS credentials `pricing:GetAttributeValues` and `bedrock-mantle:ListModels` as well.

Guards of earlier releases read only the us-east-1 prices of a version with prices per region, and keep pricing every call at them until upgraded; no upgrade order is needed between payer orgs. Rolling back to v0.2.4 is supported; it prices every call at us-east-1 prices again.

### Changed
- Each AI call is priced at its model's price in the region it was made from, as AWS bills it, instead of at the us-east-1 price everywhere. Against us-east-1, prices are typically the same in us-west-2, about 20% higher in Frankfurt, Tokyo, and Mumbai, and about 55% higher in London, so caps there were reached late. A model with no price in a call's region is priced at its highest price in any region. See `docs/ai-guard.md`.
- The price builder (`guardrail prices build` and `publish`, and the scheduled publish) reads the AWS Price List for every region, and Bedrock's and the `bedrock-mantle` endpoint's model lists in every region the account can reach. The release's table now prices 109 models in 34 regions, up from 86 models in us-east-1: models offered only outside us-east-1 (such as `deepseek.v3-v1:0` and `qwen.qwen3-coder-480b-a35b-v1:0`), mantle model ids (such as `google.gemma-4-31b` and `xai.grok-4.3`), and dated snapshots (such as `openai.gpt-5.4-2026-03-05`). No price is lower.
- A shared price version holds one chunk per region. Its us-east-1 chunk also lists each model not offered in us-east-1 at its highest price, so guards of earlier releases stop treating those models as unknown.
- A manual shared price applies in every region, and is dropped by itself only once the Price List covers the model in every region it's offered in.
- Publishing reports changes per model across regions, and names the regions whose model lists it couldn't read.

### Security-relevant
- With `PublishSharedPrices=true`, the guard's role gains `pricing:GetAttributeValues` (public data, no resource-level permissions) and `bedrock-mantle:ListModels` on the account's own mantle projects, and the scheduled publish calls Bedrock and the `bedrock-mantle` endpoint in every region once a day.
- The function's timeout is 300 seconds, up from 120, for the scheduled publish. The template revision is now 4.

## [v0.2.4] - 2026-11-17

### Upgrade
1. Update the stack with this release's package, as in [upgrading](docs/upgrade.md). The template is unchanged (revision 3).

Rolling back to v0.2.3 is supported; on a trail that logs Bedrock data events, it brings back the double count.

### Fixed
- On an organization trail that also logs Bedrock model data events (`AWS::Bedrock::Model`), CloudTrail records each `InvokeModel`, `Converse`, and streaming call twice, as a management event and a data event. The AI guard counted both, doubling every estimate and cutting accounts off at half their caps, and fired tripwires twice. It now counts each call once, from its management event. Trails that log only management events are unaffected.

### Security-relevant
None.

## [v0.2.3] - 2026-11-17

### Upgrade
1. Update the stack with this release's template and package (template revision 3), as in [upgrading](docs/upgrade.md). The AI cutoff policy is updated in place: accounts already cut off get the wider cutoff at once, with no new cutoff. A guard of this release on an older template keeps running but alerts that its template is older than it needs.

Rolling back to v0.2.2 is supported; it restores the narrower cutoff.

### Changed
- The AI cutoff now denies every way to start paid Bedrock or AgentCore work, not only model invocation: inference through the OpenAI- and Anthropic-compatible `bedrock-mantle` endpoint, Bedrock API keys on it, reranking, knowledge-base retrieval and generation, agents and flows, batch inference, provisioned throughput, model customization, Bedrock Data Automation, and mantle fine-tuning and reservations. Before this, a cut-off account could keep calling models through the `bedrock-mantle` endpoint. Reads, listing, and cleanup stay allowed, and work already running is not stopped. See `docs/ai-guard.md`.

### Security-relevant
- The AI cutoff service control policy denies more actions: `bedrock:Rerank`, `Retrieve`, `RetrieveAndGenerate`, `AgenticRetrieveStream`, `InvokeAgent`, `InvokeInlineAgent`, `InvokeFlow`, `CreateModelInvocationJob`, `CreateProvisionedModelThroughput`, `CreateModelCustomizationJob`, `InvokeDataAutomation`, and `InvokeDataAutomationAsync`, and `bedrock-mantle:CreateInference`, `CallWithBearerToken`, `CreateFineTuningJob`, and `CreateReservation`. It applies only to accounts the AI guard cuts off. The guard's role is unchanged.

## [v0.2.2] - 2026-11-16

### Upgrade
No action is required: the new setting defaults to today's behavior. Guards on this release read state from earlier ones unchanged.

### Added
- `ai.unknown_models`: what happens when an account calls a model with no price. `cutoff` (the default, as before) cuts off the account; `count` counts the call against the account's caps at the highest input and output prices the guard knows, so the client keeps working until a cap is passed. A call with no token counts still cuts off. The first counted call to each unpriced model sends one alert and writes one `unpriced_model` event. Switching to `count` is a loosening and needs approval; switching back applies at once. See `docs/ai-guard.md`.

### Security-relevant
- `ai.unknown_models` `count` lets calls to unpriced models through until a cap is passed, instead of cutting off at the first call. It's off by default and needs the loosening approval to turn on.

## [v0.2.1] - 2026-11-16

### Upgrade
No action is required. Updating the stack as in [upgrading](docs/upgrade.md) gives guards the 4 newly priced models in their built-in table, and guards on an earlier release get them from shared prices once the enterprise's prices are published again.

If a payer org publishes shared prices on a schedule (`PublishSharedPrices=true`), update that stack to this release before anyone publishes these prices, or as soon as possible after: a v0.2.0 publisher builds without the 4 models, so its daily publish would remove them from shared prices again.

### Fixed
- The price builder now prices 4 Bedrock models the AWS Price List does price but labels in ways it used to miss: `xai.grok-4.6` (listed by its model id, with no inference type), `mistral.voxtral-small-24b-2507` (its standard usage types say Voxtral Mini; its `-mantle` ones carry the id), and `amazon.nova-2-sonic-v1:0` and `amazon.nova-2-5-sonic` (speech and text token rates, priced at the higher of the two, and "Nova Sonic 2.0" against Bedrock's "Nova 2 Sonic"). The release's price table adds them; no other price changed. Models the Price List doesn't list, or doesn't price per token (Rerank, Pegasus), still have no price.

### Security-relevant
None.

## [v0.2.0] - 2026-11-16

### Upgrade
1. Update the stack with this release's template and package (template revision 2), as in [upgrading](docs/upgrade.md). A guard of this release on the v0.1.0 template keeps running but alerts that its template is older than it needs.
2. Optional, in one payer org per enterprise, to publish shared AI prices daily: update the `guardrail` CLI, run `guardrail setup` for that payer org, mint the price publisher key it prints and store it in Secrets Manager, then set `PublishSharedPrices=true` and `PricePublisherSecretArn` on that stack (see [publishing on a schedule](docs/operating.md#publishing-on-a-schedule)).

Rolling back to v0.1.0 is supported.

### Added
- Scheduled publishing of shared AI prices: with `PublishSharedPrices=true`, the guard builds the AI price table from the AWS Price List daily at 00:45 UTC and publishes it to `guardrail.prices` as `guardrail prices publish` does, carrying manual prices forward and writing nothing when nothing changed. It alerts once when a publish fails and when it recovers, when it publishes a version, when the Price List has no price for a model it hadn't reported, and when a manual price is above the Price List's. Its key's expiry is reported and warned like the reader's and writer's. `guardrail setup` prints the publisher key's reach.
- A manual price the Price List now prices at or above it on both input and output is dropped by the next publish, by hand or scheduled, since that changes no price guards use; the version's note says so, and no `--note` is needed.

### Changed
- Prices in alerts, approval requests, and `guardrail` output are written as plain decimals (`0.00006`), never in exponent form (`6e-05`).

### Security-relevant
- Optional, only with `PublishSharedPrices=true`: the guard's role gains `pricing:GetProducts` and `bedrock:ListFoundationModels` (public data; neither has resource-level permissions) and `secretsmanager:GetSecretValue` on the price publisher secret, and the guard calls the AWS Price List API and Bedrock's model list once a day. The price publisher key can read and write only `guardrail.prices`; shared prices still only tighten what guards charge.
- New optional stack parameters `PublishSharedPrices` and `PricePublisherSecretArn`. The template revision is now 2.

## [v0.1.0] - 2026-11-16

### Upgrade
The first release for deployment: install it as in [install](docs/install.md). A test install of v0.0.1 (not for deployment) upgrades with:
1. Update the `guardrail` CLI to this release.
2. Run `guardrail setup` for each payer org. It creates `guardrail.prices` once per enterprise and applies the inbox's retention.
3. Mint a reader key whose reach includes `guardrail.prices` and the payer org's inbox, as `setup` prints, put it in the reader secret, and revoke the old one.
4. Update the stack with this release's template and package, as in [upgrading](docs/upgrade.md).

### Added
- Local alerts name the accounts they mention, as `730335604511 (e2enormal)`, in the subject and the body, using the organization's account names (refreshed hourly; no new permission). Ids inside ARNs and cutoff ids, the `Error:` line, and inbox events are unchanged. Loosening approval requests name the accounts in their changes and freeze lines the same way; their payer id, reason, and payload keep bare ids. If names can't be read, alerts and approvals go out with bare ids.
- The heartbeat carries the guard's state: every scope in force with its ceiling, threshold, mode, and Budgets' actual spend; which scopes are frozen, since when, and how; the pending loosening and its approval id; excluded and unprotected accounts; and, with the AI guard on, its cutoffs and whether the trail is stalled (`state_layout` 1). Besides the daily heartbeat, the guard writes one at the end of any run that changed that state, so the newest heartbeat is always the current state. Its existing fields are unchanged.
- Config field `alert_emails`: up to 20 addresses that receive local alerts besides the stack's `AlertEmail`. The guard subscribes and unsubscribes them on the alert topic, and records each change in the inbox (`alert_recipient_subscribed`, `alert_recipient_removed`, and a daily `alert_recipient_unconfirmed`). Any change to the list needs the loosening approval.
- Once a day the guard reads its reader and writer keys' expiry from Jennah and alerts locally 30, 14, 7, 3, and 1 days before a key expires. The heartbeat event lists both keys' labels and expiry. Needs Jennah v0.192.0 or later; with an older Jennah the expiry is recorded as unknown and nothing is alerted.
- A reinstalled guard (stack deleted and created again) resumes from the last config version a guard applied in full, read from the inbox, so a loosening that was waiting for approval keeps waiting instead of applying at once (on the same approval, found through the earlier install's `loosening_raised` event), and a rejected one stays rejected. Its first run writes a `guard_installed` event, and its events about config versions carry the install in their ID (`config_applied:10:i<install>`), so they're recorded beside the earlier install's. Guards already installed keep their event IDs.
- A loosening approval shows the config version's note as the reason given (or says none was given), carries it in its payload, and the `loosening_raised` event records it.

- The AI guard (opt-in with `EnableAIGuard`): reads the organization CloudTrail trail every minute, prices Bedrock calls from their token counts, and cuts off an account's AI access when it passes an hourly, daily, or monthly cap, or calls a model or region not allowed, uses a Bedrock API key without being allowed to, or changes Bedrock's logging settings. Configured in the config's new `ai` section; lifts go through the loosening approval. Writes cutoff, lift, trail-stall, and hourly usage events. See `docs/ai-guard.md`.
- `guardrail preflight --ai` checks that the organization trail can feed the AI guard.
- Loosening approvals are written for the approver: a title naming the kind of change, one sentence per change, what approving would do to a frozen scope (with its Cost Explorer spend, read once per frozen scope the change touches), and the delay, earliest apply time, and expiry in words and UTC. The payload adds `earliest_apply_at` and `expires_at`; `guardrail diff` and inbox events are unchanged.
- A config with more than 100 enforcing scopes is invalid, since each needs a budget action in the payer account and AWS allows 100. `guardrail preflight` also counts budget actions outside the guard and fails when they and the config's enforcing scopes exceed 100 (needs `budgets:DescribeBudgetActionsForAccount` for the operator). When AWS refuses a scope's action at the limit, the guard alerts once naming the scopes that can't freeze, instead of a generic failure every run.
- Shared AI prices: `guardrail prices publish` writes a versioned price table, built from the AWS Price List API, to the enterprise's `guardrail.prices` dataset, which `setup` creates once per enterprise. Guards read it on the 5-minute pull and merge it with their release's table tighten-only, record each version they start using as `prices_applied`, and name the price version in AI cutoff events. `guardrail prices build` regenerates the release's table. The MSP id `prices` is reserved.
- Upgrades: every release's CHANGELOG entry has an Upgrade section (the release workflow refuses a tag without one) and commits a fixture of its stored state, which the release's tests load and the previous release's tests read, so upgrades and one-release rollbacks are tested. `guardrail versions --msp ID` lists the release each payer org runs, its code hash, and its newest heartbeat, against the latest release on GitHub, with guards behind first; the Jennah console shows the same. The guard alerts once, and records it in its heartbeat, when its stack template is older than its code needs (`TEMPLATE_REVISION`) or the `GuardVersion` parameter doesn't match the running code, and writes a heartbeat on its first run after its code changes. See `docs/upgrade.md`.
- Manual shared prices: `guardrail prices publish --overrides FILE` prices models the Price List build missed, for every guard in the enterprise, and raises (never lowers) ones it priced. Manual prices are kept in the version and carried into later publishes until dropped with `--drop-override`; adding, changing, or dropping one needs `--note`. The command warns when a manual price is below the floor guards charge for a model their release doesn't list. Guards are unchanged.

### Security-relevant
- `guardrail versions` (run on the operator's machine, not by the guard) reads GitHub's public releases API to find the latest release, as does the Jennah console. Guards make no new outbound calls.
- The guard's role can attach the two freeze policies (`organizations:AttachPolicy`, on the same policies, accounts, and OUs it could already detach them from), and read Cost Explorer (`ce:GetCostAndUsage`, which has no resource-level permissions). The guard attaches a freeze policy only to a scope in `enforce` whose Cost Explorer cost is at or over its threshold, which is what the Budgets action already does.
- New outbound call: Cost Explorer's `GetCostAndUsage` in us-east-1, billed at $0.01 per call, made only for lift decisions and for up to 48 hours after the guard changes a budget.
- New SCP, created only with `EnableAIGuard=true`: `guardrail-ai-cutoff-<msp id>`, denying Bedrock model invocation, Bedrock API keys, and AgentCore invoke and session actions. The guard attaches it to accounts (never OUs) itself, so its role gains `organizations:AttachPolicy` and `DetachPolicy` on that policy and on the organization's accounts.
- With `EnableAIGuard=true`, the guard's role can list and read the organization trail's bucket (`s3:ListBucket`, `s3:GetBucketLocation`, and `s3:GetObject` under `AWSLogs/`), can read the organization's id (`organizations:DescribeOrganization`, to find the trail's key layout), can update items in its own state table (`dynamodb:UpdateItem`, for the spend counters), and, when `TrailKmsKeyArn` is set, `kms:Decrypt` on that key through S3 only. Raw trail records and prompts are not sent to Jennah; only cutoffs and hourly aggregates are.
- New stack parameters `EnableAIGuard`, `TrailBucket`, `TrailPrefix`, and `TrailKmsKeyArn`. The state table now expires items with an `expires` attribute (usage counters, after about 40 days).
- New outbound call: once a day, `WhoAmI` to the Jennah endpoint the guard already uses, once with each of its keys. It needs no permission beyond the key and returns only that key's metadata.
- Config in Jennah can now direct local alerts to email addresses. Adding or removing an address needs the same approval as a loosening, from the approvers set in the stack, including in a first config. Config can't remove the stack's `AlertEmail`, a subscription made outside the guard, or one that existed before config listed it.
- The guard's role can subscribe, unsubscribe, and list subscriptions on the stack's alert topic (`sns:Subscribe`, `sns:ListSubscriptionsByTopic`, and `sns:Unsubscribe` on the topic; AWS authorizes `Unsubscribe` against the topic, so a grant on its subscriptions alone is refused), and no other topic.
- The guard function receives the stack's `AlertEmail` as `ALERT_EMAIL`, so it can refuse to unsubscribe it.
- The guard's reader key now also reaches the enterprise's `guardrail.prices` dataset, read-only. Shared prices can only raise a price or add a model, never lower one, so whoever can write that dataset can make the AI guard stricter but not looser. Existing reader keys need reminting with the added dataset; until then the guard alerts once and uses its release's prices.
- The guard's reader key now also reads the payer org's inbox (`datastore.data:read` on `guardrail.<msp>.<payer>.inbox`), so a reinstalled guard can find the version that was in force. It still can't write anything. A reinstall trusts the inbox's record of the applied version, so whoever can write the inbox can choose which published version a reinstall resumes from. Existing reader keys need reminting with the inbox; until then a reinstall alerts once and applies the latest version as before.
- Loosening approvals now state their quorum rule, derived from `LooseningRequiredApprovals` and `LooseningApprovers`: 1 is "any one approver", and the number of approvers is "all approvers". `LooseningRequiredApprovals` accepts only those two values; a stack that set it to anything else (for example 2 of 3 approvers) can't approve a loosening until it's changed. Approver emails are trimmed and counted once each.

### Changed
- The install guide's sample IAM policy for the payer account now also protects the guard function (its code, configuration, and concurrency), the outbox's event source mapping, the schedules, the stack's roles (including creating a role with a matching name), the queue, and the topics, and its exception covers only the stack's own roles instead of any role named `guardrail-*`. A payer account protected with the earlier sample could still loosen protection by changing the function's configuration.
- Local alert bodies read as full sentences: they start with a capital letter and end with a period, name targets as "Account <id>" or "OU <id>" instead of `account:<id>`, and give lift reasons in words instead of codes such as `budget_raised`. Error detail is no longer mixed into a sentence; it follows on a last line starting with `Error: `. Subjects are unchanged, so filters on subjects keep working; filters on body text need updating.
- The config-refresh reminder and recovery alerts give the outage length in words ("6 hours" instead of `6h0m0s`) and the start time as "14:45 UTC on 2026-11-12" instead of RFC 3339.

### Fixed
- A `stay_frozen` freeze is kept across a new budget period even if AWS Budgets resets its executed action then, which detaches the freeze policy: the guard attaches it again within one pull and records `freeze_carried` with `reason: period_reset`. Before, the scope could spend again, and the daily drift check reported it as detached by a person and cleared the guard's record of the freeze. A policy detached by hand while the action still reads as executed is still drift.
- A config change to a scope frozen by its AWS Budgets action unfroze it. Updating a budget whose action has executed makes AWS reset the action, which detaches the freeze policy within a second, while the guard, the inbox, and the freeze alert still said the scope was frozen, and nothing froze it again until the next budget period. The guard now waits for the reset to finish and attaches the policy again unless the change lifts the freeze, recording `freeze_carried`. A lift whose policy the reset already detached succeeds, recorded with `how: already_detached`.
- A scope frozen, lifted, and frozen again in the same budget period lost its second `freeze` event: every freeze in a period had the same event id, and Jennah keeps one row per id, so the inbox showed the scope as lifted while its freeze policy was attached. A freeze event's id now includes when that freeze began (`freeze:<scope>:<period start>:<unix seconds>`). A repeat of a freeze already in force, such as a duplicate budget notification or the Budgets action firing after the guard froze the scope, reuses the id and sends no second alert, and writes the event if its first write was lost.
- AWS Budgets resets a budget's spend to zero whenever it's created or updated, until it recalculates, which can take about a day. Two things went wrong in that window. A frozen scope whose ceiling was raised, but not above its spend, kept its freeze on the run that applied the raise and lost it on the next, because the updated budget read 0. And a scope whose budget had just changed couldn't freeze, even when it was already over its new threshold. Lifts are now decided on Cost Explorer's figure, and after the guard changes a budget it checks Cost Explorer about once an hour for up to 48 hours, freezing the scope itself (or recording `would_freeze` in dry-run) when it's over. Freeze, `would_freeze`, and lift events record `spend_source`, and freezes record `how` the policy was attached.
- Jennah refused every loosening approval the guard raised, because the request had no quorum rule, so no loosening could apply: no raised limit, lifted freeze, removed scope, or changed alert recipient. The guard now asks for "any one approver" when `LooseningRequiredApprovals` is 1, and "all approvers" when it equals the number of `LooseningApprovers`. Any other value raises no approval and alerts with the values that work, and the stricter config stays in force.
- `guardrail preflight` reported an account or OU as having no free SCP slot at 5 attached policies. AWS now allows 10, so the check and the install docs use 10.
- The guard rewrote an enforcing scope's freeze action whenever any part of the scope changed, for example only its limit. It now writes the action only when its threshold, policy, target, execution role, approval model, or subscribers differ from config.
- The guard no longer replaces each budget's notification on every run. AWS Budgets omits a percentage threshold type from what it returns, so the guard saw every notification as changed, and replacing it reset the budget's calculated spend to zero, which kept spend from ever reaching the threshold.

## [v0.0.1] - 2026-11-10

Test release of the release workflow. Not for deployment.

### Security-relevant
- First implementation. The freeze policies, IAM roles, and stack parameters it creates are described in docs/install.md.
- The guard's role can read its own Lambda configuration (`lambda:GetFunction`, on its own function only) to report its code hash.
- The guard's role can list the organization's accounts and their parents (`organizations:ListAccounts`, `organizations:ListParents`) for the coverage check.
- Optional stack parameters `ExpectedPayerAccountId` (refuses the stack in any other account) and `ReactToAccountEvents` (adds an EventBridge rule that invokes the guard).
