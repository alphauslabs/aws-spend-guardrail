# Spec Delta

## ADDED Requirements

### Requirement: The stack accepts an install code
The template SHALL accept an optional, hidden parameter `InstallCode` of the form `jgi1,<reader key>,<writer key>`, where each key is a Jennah API key. A value of any other form SHALL be refused before any resource is created. The value SHALL NOT appear in stack outputs, events, or the function's environment.

#### Scenario: A valid code
- **WHEN** a stack is created with `InstallCode` set to `jgi1,jennah_sk_<reader>,jennah_sk_<writer>` and no secret ARNs
- **THEN** the stack is created, and describing the stack shows the parameter masked

#### Scenario: A malformed code
- **WHEN** `InstallCode` is set to a single key, or to two keys without the `jgi1,` prefix
- **THEN** CloudFormation refuses the stack and creates nothing

### Requirement: The stack stores an install code's keys itself
When `InstallCode` is set, the stack SHALL create two Secrets Manager secrets in the payer account, named under `guardrail/<MSPId>/` and unique to the stack, holding the reader and writer keys, and the guard SHALL read its keys from them. Deleting the stack SHALL delete both secrets.

#### Scenario: The guard starts from an install code
- **WHEN** a stack created with an install code runs its first pull
- **THEN** the guard reads the config with the reader key and writes events with the writer key, both from the secrets the stack created

#### Scenario: Removal
- **WHEN** a stack created with an install code is deleted
- **THEN** both secrets it created are deleted with it, and secrets it did not create are left alone

### Requirement: An install code and secret ARNs are exclusive
A stack SHALL take its keys from exactly one source: an install code, or both `ReaderSecretArn` and `WriterSecretArn`. A stack given both sources, or neither, or only one ARN, SHALL be refused before any resource is created.

#### Scenario: Both given
- **WHEN** a stack is created with `InstallCode` and `ReaderSecretArn` both set
- **THEN** CloudFormation refuses it, saying to use one or the other

#### Scenario: Existing stacks keep their ARNs
- **WHEN** a stack installed with two secret ARNs is updated to the new template without an install code
- **THEN** the update proceeds and the guard keeps reading the same secrets

### Requirement: A link can point at the hosted release files
`guardrail install-link` SHALL accept a hosted option in place of a bucket and prefix. With it, the link SHALL name the template and package of the given version in the hosted release bucket. A link SHALL NOT carry an install code.

#### Scenario: A hosted link
- **WHEN** an operator runs `guardrail install-link --msp msp_a --payer 123456789012 --hosted --version v0.3.0`
- **THEN** the URL names `guard/v0.3.0/template.yaml` in the hosted bucket, with `FunctionPackageBucket` the hosted bucket and `FunctionPackageKey` `guard/v0.3.0/guard.zip`

#### Scenario: An install code given to the command
- **WHEN** a value starting with `jgi1,` or `jennah_sk_` is passed for any option
- **THEN** the command fails without printing a URL
