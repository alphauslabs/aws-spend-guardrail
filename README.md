# aws-spend-guardrail

Budget caps for AWS organizations that block new spend in an account or OU when it hits its limit, without stopping or deleting running resources.

Each management account runs a guard stack. AWS Budgets tracks spend and attaches a freeze SCP when a budget hits its threshold, so enforcement does not depend on any external service. Jennah stores the config, handles approvals for relaxing limits, and logs freeze and unfreeze events.

> **Status:** planning. No code yet. See [`add-budget-freeze-guard`](openspec/changes/add-budget-freeze-guard/) for the initial proposal.

## How it works

```
guardrail CLI (pull, edit, publish)
        |
        v
distributor's Jennah enterprise
  guardrail.<msp>.config   versioned config documents
  guardrail.<msp>.inbox    freezes, lifts, approvals, drift
        ^                         ^
        | pulls config            | writes events
        | every 5 min             |
payer (management) account, one stack per AWS organization
  guard Lambda
        | creates and updates
        v
  AWS Budgets, one budget per account or OU
        | action at the threshold
        v
  freeze SCP attached to the account or OU
```

- **Freeze Policies:** A freeze SCP blocks actions that create new cost. Style A denies specific cost-creating actions. Style B denies everything except reads, teardown actions (stop, terminate, and delete), and support, billing, and security tooling.
- **Dry-Run Mode:** New scopes start in dry-run mode and log `would_freeze` events until switched to enforce.
- **Asymmetric Updates:** Lowering a limit takes effect within minutes. Raising a limit, removing a scope, or switching to dry-run requires approval in Jennah (from approvers defined in the CloudFormation stack) followed by a cooldown delay.
- **Offline Enforcement:** If Jennah is unreachable or a new config is invalid, the guard keeps the last valid config. Because AWS Budgets applies the SCP directly, freezes still trigger even if Jennah is down.

## Roadmap and limitations

Planned for later changes:

- A Jennah agent that adjusts working limits within a set range and prompts before freezing unusual spend.
- Sub-minute cutoff for AI services (Bedrock and AgentCore).
- Running the guard Lambda in a linked account instead of the management account.

Limitations:

- Existing resources continue to accrue usage charges while frozen.
- Actions taken through AWS service-linked roles (such as Auto Scaling) bypass SCPs.
- AWS Budgets billing data lags actual spend by several hours.

## Repository layout

- `openspec/`: Specs and change proposals managed with [OpenSpec](https://github.com/Fission-AI/OpenSpec).

Planned directories for the initial implementation: `guard/` (CloudFormation template and Go Lambda), `config/` (schema and examples), `schema/` (Jennah table definitions), and `cmd/guardrail/` (CLI).

## License

[Apache License 2.0](LICENSE).
