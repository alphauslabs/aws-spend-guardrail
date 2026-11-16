# Design

## Context

`AIDenyPolicy` in `guard/template.yaml` is a service control policy the stack creates when `EnableAIGuard` is true, and the guard attaches to one account at a time. It denies `bedrock:InvokeModel`, `bedrock:InvokeModelWithResponseStream`, `bedrock:CallWithBearerToken`, `bedrock-agentcore:Invoke*`, and `bedrock-agentcore:Start*`. AWS's machine-readable IAM reference (servicereference `bedrock` and `bedrock-mantle`, read on 2026-11-16) maps operations to actions:

| Operation | Needs |
|---|---|
| `Converse`, `ConverseStream` | `bedrock:InvokeModel`, `bedrock:InvokeModelWithResponseStream` (already denied) |
| `InvokeModelWithBidirectionalStream`, `StartAsyncInvoke` | `bedrock:InvokeModel` (already denied) |
| mantle `/v1/chat/completions`, `/v1/responses`, `/anthropic/v1/messages` | `bedrock-mantle:CreateInference` |
| `Rerank`, `Retrieve`, `RetrieveAndGenerate` (and its stream) | `bedrock:Rerank`, `bedrock:Retrieve`, `bedrock:RetrieveAndGenerate` |
| `InvokeAgent`, `InvokeInlineAgent`, `InvokeFlow` | the action of the same name |
| batch, provisioned throughput, customization | `bedrock:CreateModelInvocationJob`, `CreateProvisionedModelThroughput`, `CreateModelCustomizationJob` |

## Goals / Non-Goals

**Goals:** a cut-off account can't start paid Bedrock or AgentCore work through any endpoint; everything else in the account, and reading and cleaning up AI resources, still works.

**Non-Goals:**
- Seeing or pricing these calls. Most are CloudTrail data events, which org trails don't record by default; a separate change handles detection after this one's live check.
- Stopping work already running (batch jobs, provisioned throughput, customization). A cutoff blocks new spend, like the budget freeze.
- Denying AI services outside Bedrock and AgentCore (SageMaker, Amazon Q). The budget freeze covers them.

## Decisions

**D1. An explicit list, not wildcards.** The policy names each action:
- `bedrock:` `InvokeModel`, `InvokeModelWithResponseStream`, `CallWithBearerToken`, `Rerank`, `Retrieve`, `RetrieveAndGenerate`, `AgenticRetrieveStream`, `InvokeAgent`, `InvokeInlineAgent`, `InvokeFlow`, `CreateModelInvocationJob`, `CreateProvisionedModelThroughput`, `CreateModelCustomizationJob`, `InvokeDataAutomation`, `InvokeDataAutomationAsync`
- `bedrock-mantle:` `CreateInference`, `CallWithBearerToken`, `CreateFineTuningJob`, `CreateReservation`
- `bedrock-agentcore:` `Invoke*`, `Start*` (unchanged)

Rejected: `bedrock:Invoke*` or `bedrock:*`. A wildcard would also deny actions AWS adds later, but it also catches reads and cleanup (`InvokeBuilder` and future names are unknown) and makes the policy's effect hard to review. An explicit list reads as what it blocks; new entry points are added by a release, as here.

**D2. Updated in place.** `AIDenyPolicy`'s content changes in the template; CloudFormation updates the policy, and every account it is attached to gets the new content at once. Cutoff records, ids, and events don't change. No guard code changes.

**D3. Template revision 3.** The guard's code doesn't need the new policy to run, but a guard of this release on an older template would cut off less than its docs say. Bumping `TEMPLATE_REVISION` and `MinTemplateRevision` to 3 makes such a stack alert once (add-guard-upgrades D2), so an operator who updated only the package is told.

**D4. A test pins the list and the size.** A test in `guard/` reads `AIDenyPolicy` from the template and fails unless its actions equal D1's list, so a change to the cutoff is deliberate and reviewed, and unless the policy's JSON is under 5,120 characters, the service control policy size limit.

## Risks / Trade-offs

- AWS adds new entry points over time (mantle itself is recent). The explicit list goes stale; the release notes and the separate detection change are where new ones are caught.
- `Retrieve` is a query, but it runs an embedding model, so it is paid work and is denied.
- A workload cut off mid-batch keeps running; new batches are refused.

## Migration Plan

Update the stack with the release's template (its Upgrade section says so). Accounts already cut off get the wider cutoff immediately. Rolling back restores the narrower policy, also in place.
