# Spec Delta

## Purpose

Lets someone who doesn't use the AWS CLI install the guard in a payer account from the AWS console, through a link that fills in the CloudFormation form, without the link ever carrying a secret or installing in the wrong account.

## ADDED Requirements

### Requirement: The CLI prints a quick-create link
`guardrail install-link` SHALL print a CloudFormation console quick-create URL for us-east-1. The URL SHALL name the template in the client's S3 bucket under a given prefix, set the stack name to `guardrail-<msp id>` with any `_` turned into `-` (stack names allow only letters, digits and hyphens), set `MSPId`, `ExpectedPayerAccountId`, `FunctionPackageBucket` and `FunctionPackageKey`, and set every other parameter given on the command line.

#### Scenario: A link with everything known
- **WHEN** an operator runs `guardrail install-link --msp msp_a --payer 123456789012 --bucket guardrail-artifacts-123456789012 --prefix guard/v0.1.0 --version v0.1.0 --approvers a@example.com,b@example.com`
- **THEN** the printed URL opens the us-east-1 CloudFormation quick-create page for template `guard/v0.1.0/template.yaml` in that bucket, with stack name `guardrail-msp-a`, `FunctionPackageKey` `guard/v0.1.0/guard.zip`, `GuardVersion` `v0.1.0`, and both approvers filled in

#### Scenario: Unknown values are left for the form
- **WHEN** the operator gives no secret ARNs
- **THEN** the URL sets no secret parameters, and the console form shows them empty for the person to fill in

### Requirement: Making a link needs no credentials
The command SHALL need no AWS or Jennah credentials and SHALL make no network calls.

#### Scenario: Offline
- **WHEN** the command runs with no network and no credentials
- **THEN** it prints the URL

### Requirement: A link never carries a secret
A link SHALL contain only identifiers, ARNs, emails, and the bucket and key names. The command SHALL refuse any value that looks like a Jennah API key.

#### Scenario: A key pasted by mistake
- **WHEN** an operator passes a value starting with `jennah_sk_` for any option
- **THEN** the command fails without printing a URL, and says that keys belong in Secrets Manager

### Requirement: Inputs are checked before a link is made
The command SHALL refuse an invalid MSP id, a payer account that isn't 12 digits, an invalid S3 bucket name, a secret ARN that isn't a Secrets Manager ARN in us-east-1 of the payer account, and an approver that isn't an email address.

#### Scenario: A secret in another account
- **WHEN** `--reader-secret-arn` names a secret in an account other than `--payer`
- **THEN** the command fails naming the ARN

### Requirement: The stack refuses the wrong account
The template SHALL accept an optional `ExpectedPayerAccountId`. When it is set and differs from the account the stack is created in, stack creation SHALL fail before any resource is created. When it is empty, the check SHALL be skipped.

#### Scenario: A link opened in another account
- **WHEN** a link made for payer `123456789012` is used to create the stack while signed in to account `999999999999`
- **THEN** CloudFormation refuses the stack and creates nothing

#### Scenario: Existing installs are unaffected
- **WHEN** a stack is updated without `ExpectedPayerAccountId`
- **THEN** the update proceeds as before
