## ADDED Requirements

### Requirement: A document can't enforce more scopes than AWS allows budget actions
A document SHALL be invalid when more than 100 of its scopes are in `enforce` once defaults are applied, because AWS allows at most 100 budget actions per account and each enforcing scope needs one in the payer account. The problem SHALL be reported on `scopes`, give the number of enforcing scopes and the limit, and say to group accounts into OU scopes or set some scopes to `dry-run`. Scopes in `dry-run` SHALL NOT count.

#### Scenario: 101 enforcing scopes are refused
- **WHEN** a document has 101 account scopes and `defaults.mode` is `enforce`
- **THEN** validation fails on `scopes`, saying 101 scopes enforce and at most 100 can

#### Scenario: Dry-run scopes don't count
- **WHEN** a document has 150 scopes, 100 of them in `enforce` and 50 in `dry-run`
- **THEN** the document is valid

#### Scenario: A guard doesn't apply a refused version
- **WHEN** a version with 101 enforcing scopes is published to Jennah by a tool that skips validation
- **THEN** the guard refuses it as invalid, as for any invalid version, and keeps the config it has
