# AI guard

The AI guard caps spend on AI models in Amazon Bedrock and limits the damage from a leaked credential within minutes. AWS Budgets lags by hours, sometimes a day, which works for a monthly budget but is too slow for a leaked key or runaway loop. Instead of waiting for billing data, the AI guard reads CloudTrail events, prices each call, and cuts off an account's AI access when it passes a cap or breaks a rule.

It runs in the same stack as the budget guard, in the payer (management) account, and installs nothing in member accounts. It's off unless you turn it on.

## How it works

1. An organization CloudTrail trail records every Bedrock call in every account, including the caller, model, region, and input and output token counts. Trail files land in S3 0.5 to 5 minutes after a call, and the guard reads new files every minute.
2. The guard multiplies token counts by the model's price from the table shipped with each release (see [Prices](#prices)), and tracks each account's estimated spend for the last hour, the current UTC day, and the current calendar month.
3. When an account passes a cap or breaks a rule, the guard attaches the AI cutoff policy to that account. The policy takes effect within seconds.

From a call to a cutoff is typically 2 to 6 minutes. Nothing on this path waits for Jennah: if Jennah is unreachable, the guard keeps going on the last config, and its events reach the inbox when Jennah returns.

The guard moves past a trail file only once the spend of its calls is stored. If it can't store it (for example, its role lost write access to its state table), it alerts once (`guardrail: AI spend not counted`), reads the same files again on every run, and alerts again when storing works (`guardrail: AI spend counted again`). No call is lost; a call may be counted twice after a partial failure, which can only cut off sooner.

## What a cutoff blocks

The cutoff is a service control policy, `guardrail-ai-cutoff-<msp id>`, attached to one account at a time, never an OU. It denies every way to start paid Bedrock or AgentCore work:

- **Model calls:** `bedrock:InvokeModel` and `bedrock:InvokeModelWithResponseStream`, which also govern `Converse`, `ConverseStream`, the bidirectional stream (Nova Sonic), and async invoke.
- **The OpenAI- and Anthropic-compatible endpoint** (`bedrock-mantle`): `CreateInference`, which covers `/v1/chat/completions`, `/v1/responses`, and `/anthropic/v1/messages`, plus `CreateFineTuningJob` and `CreateReservation`.
- **Bedrock API keys**, on both endpoints: `bedrock:CallWithBearerToken` and `bedrock-mantle:CallWithBearerToken`.
- **Knowledge bases, reranking, agents, and flows:** `Rerank`, `Retrieve`, `RetrieveAndGenerate`, `AgenticRetrieveStream`, `InvokeAgent`, `InvokeInlineAgent`, and `InvokeFlow`.
- **New spend commitments:** batch inference (`CreateModelInvocationJob`), provisioned throughput, model customization, and Bedrock Data Automation.
- **AgentCore** invoke and session calls (`bedrock-agentcore:Invoke*` and `Start*`).

Everything else keeps working: EC2, S3, and every other service, and reading, listing, and cleaning up Bedrock resources (listing models, reading a batch job's status, deleting provisioned throughput). Nothing already running is stopped: a batch job or provisioned throughput started before the cutoff keeps running and billing.

When a release widens this list, updating the stack updates the policy in place, so accounts already cut off get the wider cutoff at once, with no new cutoff.

## Caps and rules

Set in the config's `ai` section ([config](config.md#ai)):

- **Caps**, per account or OU: `hourly_usd`, `daily_usd`, and `monthly_usd` of estimated spend. An account scope beats an OU scope. An account no AI scope covers has no caps.
- **Rules that cut off immediately** for every account:
  - a call to a model not on `ai.models` (when set), or to a model with no price, unless `ai.unknown_models` is `count` (see [models with no price](#models-with-no-price));
  - a call in a region not on `ai.regions` (when set);
  - a call made with a Bedrock API key by a caller not on `ai.api_key_callers`. With no list, nobody may use API keys;
  - a change to Bedrock's model invocation logging settings.
- **Mode.** Each scope has `dry-run` or `enforce`; `ai.defaults.mode` applies to accounts no scope covers. In dry-run, the guard records and alerts what it would have cut off, at most once an hour per account and rule, and attaches nothing. Start in dry-run.

## Models with no price

A model has no price when neither the release's table, the enterprise's shared prices, nor the config's `ai.prices` lists it: a model released this week, or one the Price List names in a way the builder can't match. `ai.unknown_models` sets what happens when an account calls one:

- `cutoff` (the default) cuts off the account at once, as a rule. Safest for spend, harshest for the client.
- `count` counts the call against the account's caps at the highest input and output prices the guard knows, from the release table, shared prices, and `ai.prices`, taken separately (today about $0.015 input and $0.075 output per 1,000 tokens). No priced model is cheaper, so most new models are overestimated, often many times over, and the caps trip sooner. The client keeps working until a cap is passed.

Under `count`, a call whose trail record has no token counts still cuts off, since it can't be estimated at all: models priced per image, second of video, or search would otherwise run uncapped. An allowlist (`ai.models`) still applies first; a model not on it is cut off as `model_not_allowed`.

The first time the guard counts a call to an unpriced model, it sends one alert and writes one `unpriced_model` event naming the model, the account, and the prices used. Later calls to it don't repeat them. To stop the overestimate, price the model for every guard with a manual shared price (`guardrail prices publish --overrides`, see [operating](operating.md#manual-prices)) or for one payer org in `ai.prices`; a model priced and later unpriced again is reported again.

Switching to `count` is a loosening and needs approval, even in a first config; switching back to `cutoff` applies at once.

## Lifting a cutoff

Every cutoff has an id, such as `ai:111122223333:20261111T0930`, given in its alert and its `cutoff` event. To lift it, publish a config version that adds the id to `ai.lifts`, with a reason in `--note`. That's a loosening, so it waits for the approvers set in the stack and the stack's delay. Raising the cap that was passed also lifts it once applied. If the account breaks a cap or rule again after a lift, it's cut off again with a new id.

## What reaches Jennah

The guard writes to the payer org's inbox:

- `cutoff` and `would_cutoff`, with the cutoff id, account, rule, and evidence;
- `cutoff_lifted`;
- `trail_stalled` and `trail_resumed`;
- `usage_summary` once an hour per account and model: calls, input and output tokens, and estimated USD;
- `unpriced_model` the first time it counts a call to a model with no price (`ai.unknown_models` `count`), with the model, account, caller, region, and the prices used;
- `prices_applied` when it starts using a new shared price version, with how many models it raised, added, or listed lower (ignored).

Each `cutoff` and `would_cutoff` names the shared price version in use (0 for the release's prices only).

Raw trail records and prompts never leave your AWS account.

## When the trail stops

If no new trail file arrives for 30 minutes, or the guard can't read the trail (for example after a bucket or KMS key policy changes), it alerts that AI spend is not being watched, and alerts again when files resume. Totals are kept.

## Prices

`internal/packs/ai/prices.json` lists each Bedrock model's on-demand input and output price per 1,000 tokens in us-east-1, from the AWS Price List API, with the date it was published. It uses the standard (regional) rate where AWS lists one, so estimates lean high against the cheaper global rate. It leaves out batch, provisioned, prompt-cache, and long-context rates, so a heavy long-context user is estimated low.

The enterprise's shared table in Jennah (`guardrail.prices`, published with `guardrail prices publish`, see [operating](operating.md#shared-ai-prices)) is merged in tighten-only, input and output separately:

- a model in both tables costs the higher of the two prices;
- a model only the shared table lists costs at least its provider's highest release price (or the release's highest overall, for a provider the release lacks), so a new model is never priced as cheap by mistake;
- a payer org's own `ai.prices` wins over both.

So when AWS raises a price or adds a model, publishing the shared table applies it to every guard within 5 minutes, with no stack update. When AWS lowers a price, guards keep the higher one, estimating high, until a release with the lower price is installed (or an org approves a lower `ai.prices`). A guard that can't read the shared table keeps the last version it read, or its release's prices, and alerts once.

To price a model the table lacks for every payer org at once, publish it as a manual shared price (see [manual prices](operating.md#manual-prices)); it is merged like any shared price, so it can only add or raise. To price or correct a model for one payer org, add it to `ai.prices` (and to `ai.models` if you use an allowlist). A higher price applies at once; a lower or new one needs approval, since it lets more spend through.

A call through an **application inference profile** names the profile, not the model, and the trail can't tell which model is behind it. Allowlist and price the profile's ARN in config, or such calls trip as unknown.

## Costs

The AI guard reads the organization trail's S3 files: the first copy of management events is free, and the guard makes one S3 list request per active account and region a minute (one that had a file in the last 24 hours), one list per account every 10 minutes to find new regions, and one read per new file. It doesn't create a trail. A second trail would bill every management event again, at $2 per 100,000.

The list requests are most of the cost, and they grow with the number of active account and region pairs, not with Bedrock use: about $0.22 a month for each pair at S3's list price ($0.005 per 1,000). Measured on a test organization of 10 accounts with 39 pairs: about 40 lists and one file read a minute, about $8.60 a month for S3, plus about $0.15 for the function. An organization of 1,000 accounts with 5 active regions each would list about $1,000 a month.

## Enabling it

You need an **organization trail** that records management events (both read and write) in all regions. AWS Control Tower creates one.

1. Check it, with credentials for the payer account:

   ```
   guardrail preflight --msp msp_a --ai --trail-bucket <bucket> [--trail-prefix <prefix>] [--trail-kms-key <key arn>] config.json
   ```

   It finds the trail, checks that it is multi-region, logging, and records all management events, and reads one recent file. It runs with your credentials; the guard's own access is confirmed by its first run.

2. If the trail's **bucket is in another account** (Control Tower's log archive account), add to that bucket's policy:

   ```json
   {
     "Sid": "GuardrailReadTrail",
     "Effect": "Allow",
     "Principal": { "AWS": "<guard role arn, from the stack's resources>" },
     "Action": ["s3:ListBucket", "s3:GetBucketLocation", "s3:GetObject"],
     "Resource": ["arn:aws:s3:::<bucket>", "arn:aws:s3:::<bucket>/AWSLogs/*"]
   }
   ```

3. If the trail's files are **encrypted with a KMS key**, set `TrailKmsKeyArn`, and if the key is in another account, add to its key policy:

   ```json
   {
     "Sid": "GuardrailDecryptTrail",
     "Effect": "Allow",
     "Principal": { "AWS": "<guard role arn>" },
     "Action": "kms:Decrypt",
     "Resource": "*",
     "Condition": { "StringLike": { "kms:ViaService": "s3.*.amazonaws.com" } }
   }
   ```

4. Update the stack with `EnableAIGuard=true`, `TrailBucket`, and if needed `TrailPrefix` and `TrailKmsKeyArn` ([install](install.md#stack-parameters)).
5. Publish an `ai` section in dry-run, watch the `would_cutoff` events and summaries for a while, then switch scopes to `enforce`.

## Limits

- **Detection delay:** CloudTrail delivers in batches, so a cutoff typically lands 2 to 6 minutes after spend starts. A call in a region the account hasn't used in the last 24 hours is found at the guard's next 10-minute scan for new regions, so its cutoff can take up to about 16 minutes (13.5 minutes measured). Leaked spend is bounded by the burn rate in that window. Bedrock's token quotas can't be lowered below AWS's defaults, so the bound depends on how fast the guard sees the calls.
- **Estimated spend:** Prices are list prices; the AWS bill and the budget guard remain authoritative.
- **Not covered:** Bedrock Agents, knowledge bases, flows, and async invokes (CloudTrail records them only as paid data events), and AgentCore runtime and session calls themselves, which aren't in the trail. Models AgentCore calls through Bedrock are counted, and the cutoff blocks AgentCore.
- **SCP limit:** An account with 10 SCPs attached directly can't take the cutoff; the guard alerts when an attach fails.
