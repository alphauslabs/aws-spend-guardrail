# Proposal

## Why

The AI guard can't count four kinds of Bedrock use: agents (and flows), knowledge-base retrieval, bidirectional streams (Nova Sonic), and async invokes. CloudTrail records them only as data events without token counts, and they publish no usage the guard can read. A cap is never reached through them; the cutoff blocks them only once an account is cut off for something else. An operator who doesn't use them has no way to rule them out, so a leaked credential or a runaway job can spend through them without limit.

## What Changes

- New `blocked` setting in `ai.defaults` and in each AI scope: a list of `agents`, `knowledge_bases`, `bidirectional_streams`, and `async_invoke`. A scope's list replaces the defaults' list; an empty list blocks nothing. Unset everywhere, nothing is blocked, as today.
- The guard keeps blocked use denied in the covered accounts of `enforce` scopes, with one service control policy, `guardrail-ai-blocked-<msp id>`, that the stack creates and the guard attaches to the organization root only while something is blocked, and rewrites to list the accounts per kind. The management account is never affected, as SCPs don't apply to it.
- Blocking applies on the guard's next pull. Allowing (removing a kind from a list, or a scope's list) is a loosening: it needs approval and the stack's delay, and the approval says the allowed use isn't counted toward caps.
- What each kind denies: agents and flows by their actions (`bedrock:InvokeAgent`, `InvokeInlineAgent`, `InvokeFlow`), knowledge bases by theirs (`bedrock:Retrieve`, `RetrieveAndGenerate`, `AgenticRetrieveStream`), async invokes as `bedrock:InvokeModel` on `async-invoke` resources, and bidirectional streams as `bedrock:InvokeModel` on the Nova Sonic models, since AWS authorizes a stream as `InvokeModel` on its model.
- An inbox event records each change to what is blocked where.
- Docs and the config schema.

## Capabilities

### New Capabilities

### Modified Capabilities

- `guard-config`: the AI section accepts `blocked`, validated, and classified as tightening when it adds and loosening when it removes.
- `ai-pack`: blocked use is denied in the covered accounts of enforce scopes, with a standing policy kept in step with config.

## Impact

- `config` (`ai.go`, `aidiff.go`, `sentence.go`, `schema.json`): the field, its resolution, validation, change classification, and approval sentences.
- `internal/guard`: computing each account's blocked kinds from the AI scopes, building the policy, attaching, updating, and detaching it; `internal/awsx`: `UpdatePolicy`, root lookup.
- `guard/template.yaml`: the `AIBlockPolicy` resource and the guard role's permission to update it and attach it to the root; template revision 6.
- Docs: `config.md`, `ai-guard.md`, `aws-facts.md`, `uninstall` notes; CHANGELOG.
- A payer org that sets `blocked` has one more SCP at its organization root while anything is blocked; the root takes at most 10 SCPs. The policy's 5,120-character limit caps it at about 300 accounts with any block (accounts with the same kinds share a statement); beyond that the guard alerts and keeps the last policy it could write.
