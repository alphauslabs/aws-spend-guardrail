# Proposal

## Why

The AI guard prices Bedrock calls from a table compiled into each release. A new Bedrock model therefore trips as unknown until a release adds it, and every price update means redeploying the guard's stack in every payer org, one stack per organization. With MSPs running many payer orgs, keeping prices current becomes release and rollout work, and a future compute pack (EC2 and GPU launches priced per instance type and region) multiplies the table by about 20 times.

Prices are better kept like config: published once in Jennah and read by every guard. But a shared table reaches every client at once, so it must never be able to weaken protection on its own: a wrong or malicious table setting prices near zero would quietly disable every client's caps, with no approval anywhere.

## What Changes

- **A shared `guardrail.prices` dataset per Jennah enterprise,** holding versioned price tables: a small manifest row per version listing chunks, and one chunk row per pack and region. A chunk is fetched by key, so the layout scales to compute prices without large rows or capped query results.
- **Guards read it on their 5-minute pull,** download only chunks whose hash changed, for the packs they run and the regions they've seen activity in, and keep the last good copy when Jennah is unreachable or a version is invalid.
- **Shared prices can only tighten.** For a model the release table knows, the guard uses the higher of the two prices. A model only the shared table lists is priced at no less than the most expensive model of the same provider in the release table. Price decreases arrive with a release, or through a payer org's own `ai.prices`, which needs that org's approval.
- **The guard records which price version it uses:** an inbox event when it starts using one, and the version in each cutoff.
- **An ops command** builds a price version from the AWS Price List API and publishes it, with a dry run showing what changes.
- **The reader credential also reads `guardrail.prices`.** `setup` creates the dataset once per enterprise.
- **One table per Jennah enterprise, shared by every MSP and payer org in it.** It holds AWS list prices, which are the same for everyone; anything specific to a payer org stays in that org's `ai.prices`. Where several enterprises run guards, each has its own `guardrail.prices`, and publishing names the enterprise.
- **`prices` becomes a reserved MSP id,** so no MSP's datasets (`guardrail.<msp>.<payer>.*`) can be confused with the shared `guardrail.prices`.

Non-goals: per-MSP price tables (an optional `guardrail.<msp>.prices` layered on top is a possible later change); the compute pack itself; per-region AI prices (the AI chunk stays us-east-1 for now, the layout allows more); letting shared prices lower anything; and fetching prices from AWS inside the guard.

## Capabilities

### New Capabilities

- `shared-prices`: the shared price dataset, its versions and chunks, how guards read and merge it, and the tighten-only rule.

### Modified Capabilities

- `guard-jennah-contract`: the reader credential's reach adds the prices dataset, `setup` creates it, and the inbox gains a price-version event.

## Impact

- **Code:** `schema` (dataset and table layouts, credential reach, `prices` reserved as an MSP id), `internal/jennah` (reading the latest manifest and chunks by key, publishing a version), `internal/packs/ai` (merging the shared table), `internal/guard` (pulling and keeping prices), `internal/cli` and `cmd/guardrail` (`setup`, and a new `prices publish` command), a price builder in `internal/prices`.
- **Jennah:** no platform change expected. Task 1.1 confirms the largest row and message size Jennah accepts.
- **Docs:** `docs/ai-guard.md` (prices), `docs/operating.md` (minting the reader key, publishing prices), `CHANGELOG.md` (Security-relevant: the reader key reads a second dataset, and shared prices can only tighten).
- **Existing installs:** a reader key minted before this change can't read the prices dataset. The guard then keeps using its release table and alerts once, until the key is rotated with the wider reach.
