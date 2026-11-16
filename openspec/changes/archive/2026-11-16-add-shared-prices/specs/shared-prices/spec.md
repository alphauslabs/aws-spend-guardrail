# Spec Delta

## Purpose

Keeps the guard's price tables in Jennah, published once and read by every guard, so new models and higher prices reach every payer org without a release, while a shared table can never lower a price or otherwise let more spend through on its own.

## ADDED Requirements

### Requirement: Prices are published as versions of chunks
A price version SHALL be one manifest row listing its chunks, each chunk identified by pack and region and carrying a hash of its content, and one chunk row per pack and region holding that chunk's prices. A version, once published, SHALL NOT change. A chunk SHALL NOT exceed 1 MiB, and publishing SHALL refuse a version with a larger chunk.

#### Scenario: Publishing a version
- **WHEN** an operator publishes prices with one AI chunk for us-east-1
- **THEN** the dataset gains a new version whose manifest lists that chunk and its hash, and the chunk can be read by its version, pack, and region

#### Scenario: An oversized chunk
- **WHEN** a chunk would exceed 1 MiB
- **THEN** publishing fails naming the chunk, and no version is written

### Requirement: Guards fetch only what changed
On each pull, the guard SHALL read the latest manifest, and SHALL download a chunk only when it runs that pack, needs that region, and the chunk's hash differs from the one it holds. A chunk whose content doesn't match its manifest hash, or doesn't parse, SHALL be ignored and alerted, and the guard SHALL keep its previous prices.

#### Scenario: Nothing changed
- **WHEN** the latest manifest's chunk hashes match what the guard holds
- **THEN** the guard downloads no chunk

#### Scenario: A corrupt chunk
- **WHEN** a chunk's content doesn't match its hash
- **THEN** the guard keeps its previous prices for that chunk and alerts once

### Requirement: Shared prices can only tighten
The guard SHALL price a model the release table knows at the higher of the release price and the shared price, separately for input and output. A model only the shared table lists SHALL be priced at no less than, separately for input and output, the highest price of any model from the same provider in the release table, or of any model in the release table when the provider has none there. A payer org's `ai.prices` override SHALL still take precedence, as it is approved per org.

#### Scenario: A higher shared price
- **WHEN** the release prices a model at $0.003 per 1,000 input tokens and the shared table at $0.004
- **THEN** the guard prices input at $0.004

#### Scenario: A lower shared price
- **WHEN** the shared table lists a model at half its release price
- **THEN** the guard keeps the release price

#### Scenario: A new model
- **WHEN** the shared table lists `anthropic.claude-new-model` at $0.001 per 1,000 input tokens and the release table's most expensive Anthropic model costs $0.0055
- **THEN** the guard prices the new model's input at $0.0055, and it no longer trips as unknown

### Requirement: Prices keep working without Jennah
When Jennah is unreachable, the prices dataset is missing, or the guard's key can't read it, the guard SHALL keep its last good shared prices, or the release table alone if it has none, and SHALL alert once that shared prices aren't being read, and once when they are again.

#### Scenario: An old reader key
- **WHEN** a guard's reader key has no access to `guardrail.prices`
- **THEN** the guard prices with its release table and alerts once that the key needs the prices dataset in its reach
