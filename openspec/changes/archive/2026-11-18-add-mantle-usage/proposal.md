# Proposal

## Why

The AI guard prices only `bedrock-runtime` calls, from the trail's token counts. Inference through the `bedrock-mantle` endpoint (OpenAI Chat Completions and Responses, and Anthropic Messages) is a CloudTrail data event with no token counts, so the guard doesn't count it: a cap is never reached through mantle, however much it costs. AWS now leads with mantle: its console redesigned in June 2026 is built around it, and it serves models `bedrock-runtime` doesn't. Since v0.2.3 the cutoff blocks mantle, but nothing cuts off an account for mantle spend.

Live tests on 2026-11-17 found a source that works. Mantle publishes per-minute input and output token totals per account and model to CloudWatch (`AWS/BedrockMantle`), exact against what the API reported, about 24 seconds after a call. With CloudWatch cross-account observability, the payer account reads every linked member account's metrics in one query per region, labeled by account and model.

## What Changes

- The guard reads mantle usage once a minute, in each region the stack lists, from the member accounts' `AWS/BedrockMantle` metrics shared with the payer through a CloudWatch cross-account observability sink. It counts each minute's tokens once, including datapoints that grow after it first read them.
- The AI pack prices mantle usage like a runtime call: same price tables, caps, model and region allowlists, and unknown-model policy. Mantle usage is priced by its own model id at the price of its region, which add-regional-prices provides; an allowlist entry differing only by a version suffix (`openai.gpt-oss-20b-1:0`) allows the mantle id (`openai.gpt-oss-20b`).
- New stack parameter `MantleRegions` (empty by default: mantle not watched). The guard's role gains read access to CloudWatch metrics. The template revision becomes 5.
- The guard alerts once when it can't read a listed region's metrics, and again when it can. The daily coverage check reports, once each, active accounts not linked to the sink in a listed region.
- `guardrail preflight --ai` checks each listed region's sink and how many active accounts are linked to it.
- Docs: setting up the sink and organization-wide links, the delay, the cost, and what's still not covered.

## Capabilities

### New Capabilities

### Modified Capabilities

- `activity-guard`: a second input besides the trail, mantle metrics read through cross-account observability, with its own counted-once, access-alert, and coverage rules.
- `ai-pack`: mantle usage is priced, capped, and checked against the model and region allowlists.

## Impact

- `internal/activity`: a metrics reader beside the trail reader; the engine feeds its records to packs and commits it only once their usage is stored.
- `internal/awsx`: CloudWatch `GetMetricData` with a cross-account `SEARCH`, and listing a sink's attached links.
- `internal/packs/ai`: mantle records, model id matching, calls counted from `Inferences`.
- `guard/template.yaml` (parameter, permissions, revision 5), `guard/main.go`, `internal/guard` (daily link check).
- `cmd/guardrail` preflight; docs `ai-guard.md`, `install.md`, `aws-facts.md`, `upgrade.md`; CHANGELOG.
- Operators who want mantle counted must create a sink in the payer account and link member accounts in each mantle region. Cost: CloudWatch charges $0.01 per 1,000 metrics a query returns, about $1.30 a month for each account and model pair that used mantle in the last two weeks.
- Not covered still: models that publish no metrics, bidirectional streams, async invokes, agents, and knowledge bases; the API-key tripwire for mantle (the metrics carry no caller).
