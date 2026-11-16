# Proposal

## Why

The AI cutoff is meant to stop an account's AI spend at once. Its service control policy denies only `bedrock:InvokeModel`, `bedrock:InvokeModelWithResponseStream`, `bedrock:CallWithBearerToken`, and AgentCore's `Invoke*` and `Start*`. AWS's IAM reference shows several ways to start paid Bedrock work that need other actions:

- Inference through the `bedrock-mantle` endpoint, which serves the OpenAI- and Anthropic-compatible APIs (`/v1/chat/completions`, `/v1/responses`, `/anthropic/v1/messages`), needs `bedrock-mantle:CreateInference`. Most new SDKs and tools reach Bedrock this way.
- `Rerank`, `Retrieve`, `RetrieveAndGenerate`, `InvokeAgent`, `InvokeInlineAgent`, and `InvokeFlow` each need their own `bedrock:` action.
- Batch inference (`CreateModelInvocationJob`), provisioned throughput, model customization, Bedrock Data Automation, and mantle fine-tuning and reservations are also new AI spend with their own actions.

So an account the guard has cut off can likely keep using models through the mantle endpoint, agents, or knowledge bases, and start new batch or provisioned spend. A cutoff that leaves the most common modern entry point open doesn't do what the AI guard promises.

## What Changes

- The AI cutoff policy also denies every Bedrock action that starts paid model work or a new AI spend commitment: mantle inference and its API keys, Rerank, knowledge-base retrieval and generation, agents and flows, batch inference, provisioned throughput, model customization, Data Automation, and mantle fine-tuning and reservations.
- It still denies nothing outside Bedrock and AgentCore, and nothing that only reads, lists, or cleans up.
- The policy is updated in place by a stack update, so accounts already cut off get the wider cutoff without a new cutoff.
- The stack template revision goes to 3, so a guard of this release on an older template alerts that its cutoff is narrower than documented.
- Already running batch jobs, provisioned throughput, and customization jobs are not stopped, as today for the budget freeze.

## Capabilities

### New Capabilities

### Modified Capabilities

- `ai-pack`: the cutoff denies every way to start paid Bedrock work or a new AI spend commitment, not only model invocation.

## Impact

- `guard/template.yaml`: the `AIDenyPolicy` action list and `TEMPLATE_REVISION`; `internal/guard/deployment.go`: `MinTemplateRevision`.
- A template test that pins the denied actions and keeps the policy under the service control policy size limit.
- Docs: `docs/ai-guard.md` (What a cutoff blocks), CHANGELOG (Upgrade and Security-relevant).
- Detecting and pricing the calls the AI guard doesn't see today (data events) is a separate change, planned after this one's live check.
