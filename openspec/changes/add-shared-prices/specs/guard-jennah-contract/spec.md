# Spec Delta

## ADDED Requirements

### Requirement: The reader reads shared prices
The guard's reader credential SHALL reach the enterprise's `guardrail.prices` dataset with read-only data access, in addition to its payer org's config dataset, and nothing more. `setup` SHALL create `guardrail.prices` and declare its tables when the enterprise has none, leave it unchanged otherwise, and print the reader's reach including it.

#### Scenario: One table for every MSP
- **WHEN** guards of two MSPs run in the same enterprise
- **THEN** both read the same `guardrail.prices`

#### Scenario: Setting up a second payer org
- **WHEN** `setup` runs for a payer org in an enterprise that already has `guardrail.prices`
- **THEN** the prices dataset is left unchanged and the printed reader reach names both datasets

### Requirement: The prices name is reserved
An MSP id SHALL NOT be `prices`, so that no MSP's datasets can be confused with the shared prices dataset. Validation of an MSP id SHALL refuse it, naming the reason.

#### Scenario: An MSP named prices
- **WHEN** an operator runs `setup` or `install-link` with `--msp prices`
- **THEN** the command fails, saying the id is reserved for the shared prices dataset

### Requirement: The inbox records the price version in use
When a guard starts using a new price version, it SHALL write a `prices_applied` event naming the version and, per chunk it uses, how many models the shared table raised, added, or listed lower than the release (and so ignored). Each AI cutoff and would-cutoff event SHALL name the price version in use.

#### Scenario: A new version is applied
- **WHEN** a guard starts using price version 12, which raises two prices and adds one model
- **THEN** a `prices_applied` event records version 12 with two raised and one added, and later cutoffs name version 12
