# Install from the AWS console

The same install as [install](install.md), done in the AWS console instead of the AWS CLI. Sign in to the payer (management) account of the organization, with administrator access, and switch the region to **US East (N. Virginia) us-east-1** before you start.

Two things still need someone with the `guardrail` CLI, usually whoever set up the MSP in Jennah. Ask them first:

- the payer org's datasets in Jennah and its first config version, with every scope in `dry-run` ([operating](operating.md), steps 1 and 3);
- the guard's two Jennah keys, a **reader** and a **writer** ([operating](operating.md), step 2). You'll paste them into Secrets Manager below. Each key is shown only once.

## 1. Enable service control policies

The freeze is a service control policy (SCP), so SCPs must be on for the organization. If your organization already uses SCPs, skip this step.

1. Open **AWS Organizations**, then **Policies** in the left menu.
2. Choose **Service control policies**.
3. If it says service control policies are disabled, choose **Enable service control policies**.

AWS attaches its default `FullAWSAccess` policy everywhere, so nobody's access changes. The guard never turns SCPs on or off by itself ([why](install.md#service-control-policies-must-be-enabled)).

## 2. Check SCP slots

An account or OU can have at most 5 SCPs attached directly. A scope whose target already has 5 can't take a freeze policy.

1. In **AWS Organizations**, choose **AWS accounts**.
2. For each account (or OU) in your config, open it and look at the **Policies** tab, under **Service control policies**, **Attached directly**.
3. If any has 5, detach or merge one before going further.

With many scopes, `guardrail preflight` does this check for you.

## 3. Store the two Jennah keys

Do this twice, once for the reader key and once for the writer key.

1. Open **Secrets Manager** and choose **Store a new secret**.
2. Secret type: **Other type of secret**. Choose the **Plaintext** tab, clear the sample text, and paste the key (it starts with `jennah_sk_`).
3. Encryption key: keep `aws/secretsmanager`. Choose **Next**.
4. Secret name: `guardrail/<msp id>/reader` (or `writer`). Choose **Next**, leave rotation off, choose **Next**, then **Store**.
5. Open the secret and copy its **Secret ARN**. You'll need both ARNs in step 5.

## 4. Upload the function package

1. Open **S3** and choose **Create bucket**. Name it, for example, `guardrail-artifacts-<payer account id>`. Keep **Block all public access** on, and create it.
2. Open the bucket and **Upload** `guard.zip` from the release you reviewed, under a folder named after its version (for example `guard/v0.1.0/guard.zip`).
3. Note the bucket name and the object's **Key** (`guard/v0.1.0/guard.zip`).

## 5. Create the stack

1. Open **CloudFormation**, then **Create stack**, **With new resources (standard)**.
2. **Upload a template file**, choose `template.yaml` from the same release, and choose **Next**.
3. Stack name: `guardrail-<msp id>`. Fill in the parameters:

   | Parameter | Value |
   |---|---|
   | `MSPId` | the MSP id used in Jennah |
   | `JennahEndpoint` | leave empty |
   | `ReaderSecretArn`, `WriterSecretArn` | the two ARNs from step 3 |
   | `LooseningApprovers` | the emails that approve raising a limit, comma-separated |
   | `LooseningRequiredApprovals` | `1`, or `2` if the person who changes a limit must not be its only approver |
   | `LooseningApprovalWindow`, `LooseningDelay` | keep `24h` and `30m` unless you've agreed otherwise |
   | `FunctionPackageBucket`, `FunctionPackageKey` | from step 4 |
   | `GuardVersion` | the release version, for example `v0.1.0` |
   | `AlertEmail` | who receives local alerts (optional) |

4. Choose **Next** twice. On the review page, tick **I acknowledge that AWS CloudFormation might create IAM resources**, then **Submit**.
5. Wait for **CREATE_COMPLETE** on the **Events** tab (a few minutes). If you gave an alert email, confirm the subscription email AWS sends.

## 6. Check it works

Within about 5 minutes:

- **Billing and Cost Management → Budgets** lists one `guardrail-account-…` (or `guardrail-ou-…`) budget per scope.
- The guard's events appear in the payer org's inbox in Jennah, starting with `config_applied`.

Budgets calculates spend a few times a day, so the first `would_freeze` events can take hours.

## Updating or removing

- **New release:** upload its `guard.zip` under a new folder, then **CloudFormation → the stack → Update → Replace existing template** with the new `template.yaml`, and change `FunctionPackageKey` and `GuardVersion`.
- **Change approvers or the delay:** **Update → Use existing template**, and change the parameters.
- **Remove:** **CloudFormation → the stack → Delete**. Before it goes, the guard lifts every freeze it recorded and deletes the budgets it owns.

Limits themselves (ceilings, thresholds, dry-run or enforce) are changed in Jennah, not in AWS. See [config](config.md).
