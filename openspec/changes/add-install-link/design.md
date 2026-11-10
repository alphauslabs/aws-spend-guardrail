# Design

## Context

See proposal.md for why. The stack is already installable from the console by uploading `template.yaml` and filling in the form ([install from the console](../../../docs/install-console.md)). CloudFormation's console supports quick-create links:

```
https://us-east-1.console.aws.amazon.com/cloudformation/home?region=us-east-1#/stacks/quickcreate
  ?templateURL=<https URL of the template in S3>
  &stackName=<name>
  &param_<ParameterName>=<value> ...
```

The template must be in S3 and readable by the signed-in user. A private bucket in the same account works. `add-verifiable-releases` requires the template to take the package from the client's bucket and reference none of ours.

## Goals / Non-Goals

**Goals:**
- One link per payer org that a non-CLI user opens, checks, and confirms.
- Nothing secret in the link, and no way to use it in the wrong account.

**Non-Goals:**
- Hosting releases in an Alphaus bucket.
- Uploading the release files for the user. The console guide covers that in two clicks.

## Decisions

### D1. The link points at the client's bucket
The URL's `templateURL` is `https://<bucket>.s3.us-east-1.amazonaws.com/<prefix>/template.yaml`, and `FunctionPackageKey` is `<prefix>/guard.zip`. Both files come from the release the client reviewed and uploaded.

*Alternative:* a public Alphaus bucket per release, so the link works with no uploads. Rejected for now: the client would deploy our copy instead of the one they verified, which works against `add-verifiable-releases`. The guard's self-reported code hash would still let them check afterwards, so this can be revisited.

### D2. Pure string building, in `internal/cli`
`install-link` validates its inputs, builds the query with `net/url` encoding, and prints it. No AWS SDK client, no Jennah client. That keeps it usable on a laptop with no credentials, for example by Alphaus during onboarding, sending the link to the client.

### D3. The account check is a template rule
```yaml
Parameters:
  ExpectedPayerAccountId:
    Type: String
    Default: ""
    AllowedPattern: "^([0-9]{12})?$"
Rules:
  RightAccount:
    RuleCondition: !Not [!Equals [!Ref ExpectedPayerAccountId, ""]]
    Assertions:
      - Assert: !Equals [!Ref ExpectedPayerAccountId, !Ref AWS::AccountId]
        AssertDescription: This link is for another payer account.
```
Rules are evaluated before any resource is created, so a wrong-account attempt leaves nothing behind.

*Alternative:* check in the guard at runtime. Rejected: by then the stack and its policies exist in the wrong organization.

### D4. Refusing keys
Every option value is checked for the `jennah_sk_` prefix, not only the secret options, since a key pasted into the wrong flag is the likely mistake.

## Risks / Trade-offs

- [A quick-create URL longer than the browser or console accepts] → The parameters are short; the URL stays under 2,000 characters. A test checks the length with realistic values.
- [The console's quick-create URL format changes] → It has been stable for years and is what AWS's own docs and other vendors use. A broken link fails visibly, and the manual console steps still work.
- [The user's role can't read the bucket] → The console reports it when loading the template. The guide says to upload the files with the same user.
