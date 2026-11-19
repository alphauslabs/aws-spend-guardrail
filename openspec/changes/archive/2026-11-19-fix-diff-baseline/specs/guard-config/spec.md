## MODIFIED Requirements

### Requirement: The CLI validates and publishes config
The `guardrail` CLI SHALL validate a document file, show how it differs from the config the guard will compare it with, classify each difference as tightening or loosening, and publish it as the next version. That config SHALL be the applied version, or a newer published version that only tightens it, or their stricter merge while a newer version's loosening waits. Publishing SHALL refuse an invalid document and SHALL be safe to retry.

#### Scenario: Publish shows the classification
- **WHEN** an operator publishes a document that raises one ceiling
- **THEN** the CLI shows that change as loosening needing approval before it writes the new version

#### Scenario: A retried publish writes one version
- **WHEN** a publish is retried after a lost response
- **THEN** exactly one new version exists

#### Scenario: The applied report hasn't arrived
- **WHEN** version 28, which only tightens version 27, was published and applied, the inbox still reports version 27 as applied, and an operator diffs a document that undoes part of version 28
- **THEN** the CLI compares with version 28 and shows that part as loosening

#### Scenario: A loosening waits
- **WHEN** version 28 raises a cap that version 27 set and lowers another, and waits for approval
- **THEN** the CLI compares a new document with version 27 plus version 28's lowered cap, and says so
