# Install

One CloudFormation stack per payer (management) account, in us-east-1. An MSP with several payer orgs installs one stack in each, all with the same `MSPId`; each reads its own config, named by its payer account. Deploy it through your own pipeline from a version you reviewed.

## Before you start

1. [Set up the payer org in Jennah and mint the guard's two credentials](operating.md). Store them in Secrets Manager in the payer account.
2. Publish a first config with every scope in `dry-run` (the default).
3. Run `guardrail preflight` to confirm every target has a free SCP slot.
4. Build the function package with `make guard` and upload `bin/guard.zip` to an S3 bucket in us-east-1 that you control.

## Deploy

```
aws cloudformation deploy \
  --region us-east-1 \
  --stack-name guardrail-msp-a \
  --template-file guard/template.yaml \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides \
    MSPId=msp_a \
    ReaderSecretArn=<reader secret arn> \
    WriterSecretArn=<writer secret arn> \
    LooseningApprovers=finops-lead@example.com,platform-lead@example.com \
    LooseningRequiredApprovals=1 \
    FunctionPackageBucket=<bucket> \
    FunctionPackageKey=<key>/guard.zip \
    GuardVersion=<version> \
    AlertEmail=oncall@example.com
```

Within 5 minutes the guard pulls the config and creates one budget per scope. Watch the inbox in Jennah or the alert topic.

## Stack parameters

| Parameter | Default | Purpose |
|---|---|---|
| `MSPId` | | MSP id. With the payer account (taken from the stack's account), it names the Jennah datasets `guardrail.<MSPId>.<payer>.config` and `.inbox`. |
| `JennahEndpoint` | empty | Jennah gRPC endpoint as `host:port`. Empty uses Jennah's public endpoint. |
| `ReaderSecretArn` | | Secret with the reader key. |
| `WriterSecretArn` | | Secret with the writer key. |
| `LooseningApprovers` | | Emails asked to approve a loosening. |
| `LooseningRequiredApprovals` | `1` | Approvals needed. Use 2 when whoever publishes a loosening must not be its only approver. |
| `LooseningApprovalWindow` | `24h` | How long approvers have. Unanswered means not applied. |
| `LooseningDelay` | `30m` | Minimum time from first seeing a loosening version to applying it. |
| `FunctionPackageBucket`, `FunctionPackageKey` | | Where the function zip is. |
| `GuardVersion` | `dev` | Recorded with every event. |
| `AlertEmail` | empty | Optional email for local alerts. |

The approval settings are stack parameters on purpose: changing them takes a stack update in the payer account, which a Jennah credential can't do.

## What the stack creates

- Two freeze SCPs, unattached: style A (deny cost-creating actions) and style B (deny all but reads, cost-reducing actions, and support, billing and security tooling).
- The role AWS Budgets assumes to attach them.
- The guard function, run every 5 minutes and daily, and on budget notifications.
- A DynamoDB table for the guard's state, an SQS outbox for events (with a dead-letter queue and an alarm), and an SNS alert topic.
- A delete-time step that lifts every freeze the guard recorded and removes the budgets it owns.

Budgets are not stack resources. The guard creates and updates them from config, named `guardrail-<account|ou>-<id>` and tagged `guardrail:owned=true`. It never touches other budgets.

## Protect the payer account side

SCPs don't apply to the management account, so IAM there is the only lock on the guard. Restrict, to your deployment pipeline's role:

- updating or deleting this stack (it holds the approvers and the delay);
- the two secrets;
- the guard's state table and outbox queue;
- the two freeze policies.

For example, deny these to everyone except the pipeline role:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Sid": "ProtectGuardrail",
    "Effect": "Deny",
    "Action": [
      "cloudformation:UpdateStack", "cloudformation:DeleteStack", "cloudformation:CreateChangeSet", "cloudformation:ExecuteChangeSet",
      "secretsmanager:PutSecretValue", "secretsmanager:UpdateSecret", "secretsmanager:DeleteSecret",
      "dynamodb:PutItem", "dynamodb:DeleteItem", "dynamodb:UpdateItem",
      "organizations:UpdatePolicy", "organizations:DeletePolicy"
    ],
    "Resource": [
      "arn:aws:cloudformation:us-east-1:<payer>:stack/guardrail-msp-a/*",
      "arn:aws:secretsmanager:us-east-1:<payer>:secret:guardrail/msp_a/*",
      "<state table arn>",
      "<freeze policy A arn>", "<freeze policy B arn>"
    ],
    "Condition": { "ArnNotLike": { "aws:PrincipalArn": [
      "arn:aws:iam::<payer>:role/<pipeline role>",
      "arn:aws:iam::<payer>:role/guardrail-*"
    ] } }
  }]
}
```

Attach it as an IAM policy to the groups and roles that administer the payer account.

## Going to enforce

After a dry-run period whose `would_freeze` events match what you expect, publish a version that sets `mode` to `enforce` for chosen scopes. That's a tightening, so it applies on the next pull.

## Known gaps

- **Budgets lags spend by hours**, so a scope can go past its ceiling before it freezes. Use a threshold below 100%, such as 90.
- **Existing resources keep billing** during a freeze: data transfer, storage growth, Lambda triggered by existing events, on-demand DynamoDB, log ingestion. A freeze stops new resources, not all spend.
- **AWS services acting through service-linked roles are not bound by SCPs.** An Auto Scaling group, EKS managed node group or ECS capacity provider can still scale out during a freeze.
- **The payer account itself can't be frozen.** Run no workloads there.

## Remove

Delete the stack. Before the function and policies go, it lifts every freeze the guard recorded and deletes the budgets it owns. The data in Jennah stays for the record.
