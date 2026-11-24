## ADDED Requirements

### Requirement: The guard charges runtime usage the trail missed
Every 5 minutes, in each region the stack lists for mantle, the guard SHALL read Bedrock runtime's tokens per service tier only for the accounts, models, and 5-minute windows with a call the trail couldn't measure. It SHALL charge the tokens the metrics show beyond those the trail counted in that window, at that tier's price, and SHALL never refund a shortfall.

#### Scenario: A streamed Chat Completions call
- **WHEN** a linked account in a listed region makes a streamed Priority Chat Completions call, which the trail records without tokens, and the metrics show 69 input and 5 output tokens for it
- **THEN** within about 15 minutes the account's totals gain 69 input and 5 output tokens at the model's Priority price

#### Scenario: Calls the trail measured
- **WHEN** an account makes only `Converse` calls, whose records carry their tokens
- **THEN** the guard reads no runtime metrics for it, and nothing more is charged

#### Scenario: Measured and unmeasured calls in one window
- **WHEN** an account makes a `Converse` call with 100 input tokens and a streamed Chat Completions call with 69 to the same model in one window, and the metrics show 169
- **THEN** 69 input tokens are charged from the gap, and the `Converse` call isn't charged twice

#### Scenario: The metrics show fewer tokens than the trail
- **WHEN** the trail counted more tokens for an account, model, and tier than the metrics show
- **THEN** nothing is refunded, and the next window's gap is measured from zero

### Requirement: A window's runtime gap is charged once
The guard SHALL charge each account, model, and tier's gap for a window once, counting only the growth when a later read shows more. It SHALL wait long enough after a window for its trail records to arrive before charging it, SHALL keep what it has charged until usage is stored, and SHALL stop revisiting a window once it is past the mantle metrics' 15-minute reach.

#### Scenario: A late trail record
- **WHEN** a call's metrics arrive in the first minute and its trail record four minutes later
- **THEN** the call is counted once, from its trail record, and not also as a gap

#### Scenario: Usage can't be stored
- **WHEN** the guard charges a gap and can't store it
- **THEN** it charges the same gap on a later run once storing works, and only once

### Requirement: Runtime metrics problems are reported like mantle's
When the guard can't read a listed region's runtime metrics, it SHALL alert once that runtime spend the trail can't measure isn't watched there, and again when reading works. The daily check for accounts not linked to the sink SHALL cover runtime metrics as well, with one report per account and region.

#### Scenario: Unreadable metrics
- **WHEN** reading `AWS/Bedrock` metrics in a listed region fails
- **THEN** one alert says unmeasured runtime spend in that region isn't watched, and trail-measured calls are counted as before
