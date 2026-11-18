[![Default](https://github.com/alphauslabs/aws-spend-guardrail/actions/workflows/ci.yml/badge.svg)](https://github.com/alphauslabs/aws-spend-guardrail/actions/workflows/ci.yml)

## aws-spend-guardrail

Budget caps for AWS organizations that block new spend in an account or OU when it hits its limit, without stopping or deleting running resources.

Each management account runs a guard stack. AWS Budgets tracks spend and attaches a freeze SCP when a budget hits its threshold, so enforcement does not depend on any external service. An optional AI guard caps Amazon Bedrock spend within minutes, from CloudTrail. [Jennah](https://getjennah.alphaus.cloud/) stores the config, handles approvals for relaxing limits, and logs every freeze, cutoff, and config change.

> **Status:** released and running in a test AWS organization; see the [latest release](https://github.com/alphauslabs/aws-spend-guardrail/releases/latest) and the [changelog](CHANGELOG.md). Releases are built by public CI, reproducible, and signed.

Docs: [install](docs/install.md) (or [from the AWS console](docs/install-console.md)), [operating](docs/operating.md), [config](docs/config.md), [AI guard](docs/ai-guard.md), [verify a release](docs/verify.md), [upgrading](docs/upgrade.md).

## How it works

```
┌─────────────────────────────────────────────────────────────────┐
│ guardrail CLI                                                   │
│ pull, edit, and publish config; list versions                   │
└────────────────┬────────────────────────────────────────────────┘
                 │ publishes config versions
                 ▼
┌────────────────┴────────────────────────────────────────────────┐
│ Jennah enterprise                                               │
│   guardrail.<msp>.<payer>.config   versioned config documents   │
│   guardrail.<msp>.<payer>.inbox    freezes, cutoffs, approvals  │
│   guardrail.prices                 shared AI prices             │
└────────────────┬───────────────────────────────▲────────────────┘
                 │ config and prices             │ events
                 ▼ every 5 minutes               │
┌────────────────┴───────────────────────────────┴────────────────┐
│ guard Lambda, in the payer (management) account                 │
│ one stack per AWS organization                                  │
└────────────────┬───────────────────────────────┬────────────────┘
                 │ creates budgets               │ reads every minute
                 ▼                               ▼ (AI guard, optional)
┌────────────────┴──────────────┐ ┌──────────────┴────────────────┐
│ AWS Budgets                   │ │ organization CloudTrail trail │
│ one budget per account or OU  │ │ records every Bedrock call    │
└────────────────┬──────────────┘ └──────────────┬────────────────┘
                 │ at the threshold              │ cap passed or
                 ▼                               ▼ rule broken
┌────────────────┴──────────────┐ ┌──────────────┴────────────────┐
│ freeze SCP                    │ │ AI cutoff SCP                 │
│ on the account or OU          │ │ on the account                │
└───────────────────────────────┘ └───────────────────────────────┘
```

- **Freeze policies:** A freeze SCP blocks actions that create new cost. Style A denies specific cost-creating actions. Style B denies everything except reads and teardown actions (stop, terminate, and delete) of common services, and support, billing, and security tooling. Unlisted services are blocked entirely while frozen.
- **Dry-run mode:** New scopes start in dry-run mode and log `would_freeze` events until switched to enforce.
- **Asymmetric updates:** Lowering a limit takes effect within minutes. Raising a limit, removing a scope, or switching to dry-run requires approval in Jennah (from approvers defined in the CloudFormation stack) followed by a cooldown delay.
- **Offline enforcement:** If Jennah is unreachable or a new config is invalid, the guard keeps the last valid config. Because AWS Budgets applies the SCP directly, freezes still trigger even if Jennah is down.
- **AI guard (optional):** Reads the organization trail every minute, and optionally the usage of calls through the OpenAI- and Anthropic-compatible `bedrock-mantle` endpoint from CloudWatch, prices each Bedrock call from its token counts, and cuts off an account's Bedrock and AgentCore access when it passes an hourly, daily, or monthly cap, or uses a model, region, or API key it isn't allowed to. A cutoff typically lands 2 to 6 minutes after the spend starts. Bedrock use it can't count (agents, knowledge bases, bidirectional streams, and async invokes) can be blocked per scope. See [AI guard](docs/ai-guard.md).
- **Shared prices:** AI prices come from the AWS Price List, per region, and each call is priced at its region's price. One payer org per enterprise can publish them daily to Jennah, and every guard picks them up within 5 minutes. Shared prices can only raise what a guard charges, never lower it, and an operator can price a model the Price List misses by hand.
- **Verifiable releases:** Each release is built by public CI from a tag, reproducible, and signed with Sigstore. The running guard reports its version and code hash to Jennah, and `guardrail versions` lists which release each payer org runs. See [verify a release](docs/verify.md) and [upgrading](docs/upgrade.md).

## Roadmap and limitations

Planned for later changes:

- A Jennah agent that adjusts working limits within a set range and prompts before freezing unusual spend.
- Watching the Bedrock calls the AI guard doesn't see yet (bidirectional streaming, as used by Nova Sonic, async invoke, agents, knowledge bases, and Rerank).
- Updating many payer orgs' stacks at once.
- Running the guard Lambda in a linked account instead of the management account.

Limitations:

- Existing resources continue to accrue usage charges while frozen.
- Actions taken through AWS service-linked roles (such as Auto Scaling) bypass SCPs.
- AWS Budgets billing data lags actual spend by several hours.
- The AI guard estimates spend from public list prices, so private pricing and discounts aren't reflected; it errs on the high side. By default, a call to a model with no price cuts off the account; a payer org can choose to count it at the highest known price instead.

## Repository layout

- `guard/`: CloudFormation template and the Go Lambda entry point.
- `cmd/guardrail/`: the operator CLI.
- `config/`: config JSON schema, validator, and examples.
- `schema/`: Jennah dataset and table definitions.
- `internal/`: guard logic, the AI guard and its price builder, AWS and Jennah adapters, outbox, and CLI commands.
- `tools/`: release checks (changelog, pinned actions, deterministic zip).
- `docs/`: install, operating, config, AI guard, verify, and upgrade guides.
- `openspec/`: specs and change proposals managed with [OpenSpec](https://github.com/Fission-AI/OpenSpec).

Build with `make` (CLI in `bin/guardrail`, function package in `bin/guard.zip`), test with `make test`.

## License

[Apache License 2.0](LICENSE).
