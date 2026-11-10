# aws-spend-guardrail

Budget caps for AWS organizations that **freeze new spend** in an account or OU when its ceiling is reached. Nothing that is already running is stopped or deleted.

The guard runs in each payer (management) account. AWS Budgets watches spend and attaches a freeze service control policy at the ceiling, so the cap holds without any outside service in the loop. Jennah is the system of record: it holds the guard's config, the approvals for loosening it, and the record of every freeze and lift.

> **Status:** planning. There is no code yet. The first change, [`add-budget-freeze-guard`](openspec/changes/add-budget-freeze-guard/), is proposed and not implemented.

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

- **Freeze, don't stop.** A freeze policy blocks new cost. Style A denies a list of cost-creating actions. Style B denies everything except reads, cost-reducing actions (stop, terminate, delete), and support, billing and security tooling.
- **Dry-run first.** Every scope starts in dry-run and records `would_freeze` until it is switched to enforce.
- **Tighten now, loosen later.** Lowering a ceiling applies within minutes. Raising one, removing a scope or switching to dry-run waits for a Jennah approval from approvers fixed in the stack, then a delay.
- **Fails safe.** If Jennah is unreachable or a config version is invalid, the guard keeps the last valid config, and Budgets still freezes on its own.

## Not yet covered

These are planned as later changes:

- A learning agent on Jennah that adjusts a working limit inside a band and asks before freezing suspicious spend.
- A fast path for AI spend (Bedrock, AgentCore) that cuts off in under a minute.
- Running the guard's compute in a linked account instead of the management account.

Known limits of a freeze: usage of existing resources keeps billing, AWS services that act through service-linked roles (such as Auto Scaling) are not bound by SCPs, and Budgets lags actual spend by hours.

## Repository layout

| Path | Contents |
|---|---|
| `openspec/` | Specs and changes, managed with [OpenSpec](https://github.com/Fission-AI/OpenSpec) |

Planned with the first change: `guard/` (CloudFormation template and Go function), `config/` (config schema and examples), `schema/` (Jennah table layouts), `cmd/guardrail/` (CLI).

## License

[Apache License 2.0](LICENSE).
