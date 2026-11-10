# Security policy

The guard runs in the payer (management) account of an AWS organization, so we treat security reports about it as a priority.

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's private vulnerability reporting for this repository (the "Report a vulnerability" button under the Security tab). Do not open a public issue.

Include what you found, how to reproduce it, and the version or commit you tested. We aim to acknowledge a report within 3 business days and to agree on a disclosure date with you once a fix is ready.

## Scope

In scope: the guard function, the CloudFormation template, the freeze policies, the `guardrail` CLI, and how they use credentials and Jennah.

Out of scope: weaknesses in AWS services themselves, and known limits documented in the README (for example, AWS services acting through service-linked roles are not bound by SCPs).
