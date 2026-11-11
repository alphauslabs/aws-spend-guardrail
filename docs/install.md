# Install

One CloudFormation stack per payer (management) account, in us-east-1. An MSP with several payer orgs installs one stack in each, all with the same `MSPId`; each reads its own config, named by its payer account. Deploy it through your own pipeline from a version you reviewed. To do it in the AWS console instead of the AWS CLI, follow [install from the console](install-console.md).

## Before you start

1. Make sure service control policies are enabled in the organization (see below).
2. [Set up the payer org in Jennah and mint the guard's two credentials](operating.md). Store them in Secrets Manager in the payer account.
3. Publish a first config with every scope in `dry-run` (the default).
4. Run `guardrail preflight` to confirm every target has a free SCP slot.
5. Build the function package with `make guard` and upload `bin/guard.zip` to an S3 bucket in us-east-1 that you control.

### Service control policies must be enabled

The freeze is a service control policy (SCP). If SCPs are not enabled on the organization's root, AWS Budgets cannot attach a freeze policy to anything, and every freeze fails.

Check, from the management account:

```
aws organizations list-roots --query 'Roots[].[Id,PolicyTypes]'
```

The root is ready when its policy types include `SERVICE_CONTROL_POLICY` with status `ENABLED`. An empty list means SCPs are off. To enable them:

```
aws organizations enable-policy-type --root-id <root id, r-...> --policy-type SERVICE_CONTROL_POLICY
```

The guard never does this itself. It is an organization-wide decision for the organization's owner:

- AWS attaches its default `FullAWSAccess` policy to the root, every OU and every account, so nobody's access changes, but every account is evaluated against SCPs from then on.
- It is not undone when the guard's stack is deleted, since other policies may come to rely on it.
- Many organizations already have SCPs enabled, in which case there is nothing to do.

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
| `ExpectedPayerAccountId` | empty | When set, the stack can only be created in this account. Install links always set it. |
| `JennahEndpoint` | empty | Jennah gRPC endpoint as `host:port`. Empty uses Jennah's public endpoint. |
| `ReaderSecretArn` | | Secret with the reader key. |
| `WriterSecretArn` | | Secret with the writer key. |
| `LooseningApprovers` | | Emails asked to approve a loosening. |
| `LooseningRequiredApprovals` | `1` | Approvals needed. Use 2 when whoever publishes a loosening must not be its only approver. |
| `LooseningApprovalWindow` | `24h` | How long approvers have. Unanswered means not applied. |
| `LooseningDelay` | `30m` | Minimum time from first seeing a loosening version to applying it. |
| `FunctionPackageBucket`, `FunctionPackageKey` | | Where the function zip is. |
| `GuardVersion` | `dev` | Recorded with every event. |
| `AlertEmail` | empty | Optional email for local alerts. Config can add more recipients (`alert_emails`), but can never remove this one, so use an address that will stay monitored, such as a distribution list. |
| `EnableAIGuard` | `false` | When `true`, adds the [AI guard](ai-guard.md): reads the organization trail every minute and can cut off an account's Bedrock and AgentCore access. |
| `TrailBucket` | empty | Bucket the organization trail writes to. Required with `EnableAIGuard=true`. |
| `TrailPrefix` | empty | The trail's S3 key prefix, if it has one. |
| `TrailKmsKeyArn` | empty | KMS key the trail encrypts its files with, if any. |
| `ReactToAccountEvents` | `false` | When `true`, a new, invited, or moved account is checked within minutes instead of at the daily run. Only works when a CloudTrail trail in the payer account records management events; without one, account events never reach EventBridge, and the daily check is the only one. |

The approval settings are stack parameters on purpose: changing them takes a stack update in the payer account, which a Jennah credential can't do.

## What the stack creates

- Two freeze SCPs, unattached: style A (deny cost-creating actions) and style B (deny all but reads and cost-reducing actions of common services, and support, billing and security tooling; unlisted services are blocked entirely).
- The role AWS Budgets assumes to attach them.
- The guard function, run every 5 minutes and daily, and on budget notifications.
- A DynamoDB table for the guard's state, an SQS outbox for events (with a dead-letter queue and an alarm), and an SNS alert topic.
- A delete-time step that lifts every freeze the guard recorded and removes the budgets it owns.
- With `ReactToAccountEvents=true`, an EventBridge rule that runs the coverage check when accounts change.
- With `EnableAIGuard=true`, the AI cutoff SCP (unattached), a one-minute schedule to read the organization trail, and permission for the guard to read the trail's bucket (and decrypt with its KMS key, if set) and to attach or detach the AI cutoff policy on accounts only. The delete-time step also detaches every AI cutoff.

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
  "Statement": [
    {
      "Sid": "ProtectGuardrail",
      "Effect": "Deny",
      "Action": [
        "cloudformation:UpdateStack",
        "cloudformation:DeleteStack",
        "cloudformation:CreateChangeSet",
        "cloudformation:ExecuteChangeSet",
        "secretsmanager:PutSecretValue",
        "secretsmanager:UpdateSecret",
        "secretsmanager:DeleteSecret",
        "dynamodb:PutItem",
        "dynamodb:DeleteItem",
        "dynamodb:UpdateItem",
        "organizations:UpdatePolicy",
        "organizations:DeletePolicy"
      ],
      "Resource": [
        "arn:aws:cloudformation:*:<payer>:stack/guardrail-msp-a/*",
        "arn:aws:secretsmanager:*:<payer>:secret:guardrail/msp_a/*",
        "<state table arn>",
        "<freeze policy A arn>",
        "<freeze policy B arn>"
      ],
      "Condition": {
        "ArnNotLike": {
          "aws:PrincipalArn": [
            "arn:aws:iam::<payer>:role/<pipeline role>",
            "arn:aws:iam::<payer>:role/guardrail-*"
          ]
        }
      }
    }
  ]
}
```

Attach it as an IAM policy to the groups and roles that administer the payer account.

## Going to enforce

After a dry-run period whose `would_freeze` events match what you expect, publish a version that sets `mode` to `enforce` for chosen scopes. That's a tightening, so it applies on the next pull.

## What AWS charges

- **Each enforcing scope** has a budget with an action. AWS bills these action-enabled budgets at about $0.10 per budget per day, so about $3 a month each, after the first two in the payer account, whether or not the freeze ever fires.
- **Each dry-run scope** has a plain budget with a notification and no action, which costs nothing in its basic form. Going back from `enforce` to `dry-run` removes the action.
- **The function, queue, table, and topics** run a few minutes a day and cost cents a month.

Prefer OU scopes where accounts can share a ceiling: one OU scope is one budget however many accounts it covers, while 300 account scopes enforcing would cost about $900 a month.

## Known gaps

- **Budgets lags spend by hours**, so a scope can go past its ceiling before it freezes. Use a threshold below 100%, such as 90.
- **Existing resources keep billing** during a freeze: data transfer, storage growth, Lambda triggered by existing events, on-demand DynamoDB, log ingestion. A freeze stops new resources, not all spend.
- **AWS services acting through service-linked roles are not bound by SCPs.** An Auto Scaling group, EKS managed node group or ECS capacity provider can still scale out during a freeze.
- **The payer account itself can't be frozen.** Run no workloads there.

## Remove

Delete the stack. Before the function and policies go, it lifts every freeze the guard recorded and deletes the budgets it owns. The data in Jennah stays for the record.
