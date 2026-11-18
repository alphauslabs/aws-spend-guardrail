# Design

## Context

The fast path (`internal/guard/fast.go`) runs once a minute: the engine (`internal/activity/engine.go`) takes new records from the trail reader, hands each to the AI pack's `Wants` and `Evaluate`, stores usage, checks caps and tripwires, and commits the reader's position only once usage is stored. The AI pack prices a record from its model and token counts.

Facts from the live tests of 2026-11-17 (to be added to `docs/aws-facts.md`):

- `bedrock-mantle` inference is a CloudTrail data event (`CreateInference`) with model, `stream`, and `callWithBearerToken`, and no token counts. `responseElements` is null.
- `AWS/BedrockMantle` publishes `Inferences`, `TotalInputTokens`, and `TotalOutputTokens` per account, `Project`, and `Model`, and `InputTokens` and `OutputTokens` per `Project` and `Model`, in one-minute periods. The values matched the API's reported usage exactly for Chat Completions (plain and streamed) and Responses calls. A datapoint appeared 24 seconds after the call (two samples). Metrics are emitted in the region that served the call; mantle is in-region only.
- With a sink in the payer account (the organization's management account can be a monitoring account) and a link from a member account, a `SEARCH('{AWS/BedrockMantle,Model} MetricName=("TotalInputTokens" OR "TotalOutputTokens")', 'Sum', 60)` from the payer returned one series per account, model, and metric, labeled with `${PROP('AccountId')}` and `${PROP('Dim.Model')}`. The link took about 9 minutes to take effect, and for about a minute the listing and a direct query disagreed. Datapoints from before the link appeared once the model emitted a new one.
- A third-party report (June 2026) found 5 of 48 mantle models in us-east-1 with no metrics (Claude Opus 4.7 and 4.8, three Gemma 4 models). Not confirmed here: Claude isn't enabled for the test account.
- Most mantle model ids match the price table exactly; some differ by a version suffix (`openai.gpt-oss-20b` and `openai.gpt-oss-20b-1:0`, `qwen.qwen3-32b` and `qwen.qwen3-32b-v1:0`, `anthropic.claude-haiku-4-5` and `anthropic.claude-haiku-4-5-20251001-v1:0`).

## Goals / Non-Goals

**Goals:**
- Count mantle spend toward the same caps as runtime spend, within about two minutes of a call, with the same allowlists and unknown-model policy.
- Count each minute's usage once, never drop it on a failed write, and say when mantle isn't being watched.
- Keep the trail path unchanged.

**Non-Goals:**
- Models that publish no mantle metrics. Their spend stays uncounted; docs say so, and the cutoff still blocks them.
- Bidirectional streams, async invokes, agents, and knowledge bases: no usage source yet.
- The API-key tripwire for mantle: metrics carry no caller. The trail's data events do, but reading data events is a separate decision (cost).
- Per-project caps. Usage is counted per account; the `Project` dimension is ignored.
- Creating the sink or links. Operators set them up (D6), as they set up the trail.

## Decisions

### D1: Metrics become records for the same pack

A `MetricsReader` in `internal/activity` turns each new account, model, and minute's growth into an `activity.Record` with `EventSource` `bedrock-mantle.amazonaws.com`, `EventName` `CreateInference`, `EventType` `MetricDatapoint`, `EventCategory` `Metric`, the account, region, model, input and output tokens, and a new `Calls` field (the growth in `Inferences`; 1 for trail records). The engine reads both readers each tick, evaluates all records, and commits both only once usage is stored.

The AI pack then prices, caps, and checks mantle usage with its existing code: one implementation of pricing and policy, not two. `Wants` accepts these records; `Evaluate` skips the API-key tripwire for them and sets `Usage.Calls` from `Record.Calls`. Hit details say "through mantle".

The alternative, a separate usage path that produces `Usage` directly, would duplicate the allowlist, unknown-model, and pricing rules.

### D2: One SEARCH query per region per tick

Each tick, in each region in `MantleRegions`, the guard calls `GetMetricData` once from the payer account with three `SEARCH` expressions over `{AWS/BedrockMantle,Model}` for `Inferences`, `TotalInputTokens`, and `TotalOutputTokens`, `Sum`, period 60, over the last 15 minutes, each labeled `${PROP('AccountId')}|${PROP('Dim.Model')}`. Results are paged with `NextToken`.

Querying account by account would need the account list and many more calls; reading the account-level metric first to find active accounts would save little, since `SEARCH` returns only metrics with data in the last two weeks and CloudWatch charges by metrics returned.

### D3: Counted values per minute, kept in state

The reader keeps, in the activity store's meta (`mantle.<region>`), the values it has counted for each account, model, and minute in the last 15 minutes. A tick's record carries the difference between what CloudWatch reports now and what was counted. Minutes older than 15 minutes are dropped from state and no longer read. The state is saved by `Commit`, after usage is stored, so a failed write rereads the same minutes and counts the same growth again. A minute whose datapoint shrinks is ignored (CloudWatch doesn't revise sums down; a smaller value would be a partial read).

15 minutes covers the observed 24-second delay with a wide margin. A datapoint that first appears later than that is lost; docs say so.

### D4: Mantle usage is priced by its own id, at its region's price; allowlists match up to a version suffix

add-regional-prices prices mantle model ids under their own ids, per region: the price builder reads each region's mantle model list and the Price List's mantle usage types, so `openai.gpt-oss-20b`, `google.gemma-4-31b`, and dated snapshots have prices. Mantle usage is priced with `PriceIn(model, region)`, the region it was read from, like a trail record.

The model allowlist still matches up to a version suffix: an allowlist entry whose id with a trailing version suffix removed (`-v<N>:<N>`, `-<N>:<N>`, `-<YYYYMMDD>-v<N>:<N>`) equals the mantle id allows it, so allowlisting `openai.gpt-oss-20b-1:0` allows mantle's `openai.gpt-oss-20b`. Overrides and allowlists can also name the mantle id directly.

### D5: Region list as a stack parameter; access alerts like a stalled trail

`MantleRegions` is a comma-delimited stack parameter, empty by default. It's passed to the function as `MANTLE_REGIONS`. The guard's role gains `cloudwatch:GetMetricData` and `cloudwatch:ListMetrics` (no resource-level permissions) and `oam:ListAttachedLinks` and `oam:ListSinks`. These are added only when the AI guard is on. Template revision 5 (add-regional-prices took 4).

A region whose read fails (access denied, as before a new link takes effect, or throttled beyond retries) is skipped for the tick. A region without a sink doesn't fail the search: it returns no series, as if nobody used mantle (found in the live check on 2026-11-18). So each read first lists the region's sinks (`oam:ListSinks`, free) and treats none as a failed read. A failed region is skipped; on the first failure the guard alerts "mantle spend in <region> is not watched" with the error, and alerts again on the first success, tracked in meta `mantle.status`. The minutes it missed are read on recovery if still inside the 15-minute window.

A region listed but with no mantle usage returns no series and is not an error.

### D6: Linking is the operator's job; the guard checks it daily

Operators create a sink in the payer account in each listed region, with a policy allowing the organization (`aws:PrincipalOrgID`) to link metrics, and link member accounts, preferably with AWS's organization-wide CloudFormation template so new accounts are linked automatically. Docs give the steps and the template.

The daily task lists each listed region's sink's attached links (`oam:ListAttachedLinks`) and compares them with the organization's active accounts, minus excluded accounts and the payer. An account not linked is reported once per region with a `mantle_unlinked` event and alert, tracked like `unprotected_account`. `guardrail preflight --ai` prints, per listed region, whether a sink exists, and how many active accounts are linked and which aren't.

Making the guard create the sink was considered. The stack could own the sink, but links live in member accounts, so operators would still need a StackSet or the organization template; keeping both in one setup step is clearer.

### D7: Cost

`GetMetricData` costs $0.01 per 1,000 metrics returned. Three metrics per account and model pair, 1,440 ticks a day: about $1.30 a month per pair that used mantle in the last two weeks. An organization with 50 accounts each using 2 models pays about $130 a month. Docs state the formula. A slower tick for mantle was considered; the cutoff delay matters more, and the formula lets operators decide whether to list a region.

## Risks / Trade-offs

- **Missing metrics:** a model that publishes none is never counted. Mitigation: docs, and the facts table names the models reported. A later change could trip on data events for models with no metrics.
- **A new link's first minutes:** reads can be incomplete for about 10 minutes after linking. The daily check reports an account only once it's unlinked, not while a link settles.
- **Pricing differences:** if mantle and runtime prices for a model differ, the price builder already merges `-mantle` usage types into the model's key and keeps the higher price, so mantle is never underpriced by the table.
- **Rollback** to v0.2.4 means deploying its template, which has no `MantleRegions` parameter: mantle stops being counted, and the sink and links stay but go unused. Stored usage and state stay valid; the `mantle.*` meta items are ignored.
