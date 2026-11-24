## ADDED Requirements

### Requirement: An unmeasured call is charged from metrics where they are read
A call the trail can't measure SHALL NOT trip `unmeasured_call` when it is made in an account and region whose runtime metrics the guard reads; its tokens SHALL be charged from the metrics gap instead, and the call SHALL count as one call. Where the guard doesn't read them, because the region isn't listed or the account isn't linked, the call SHALL be treated as `ai.unmeasured_calls` says.

#### Scenario: A linked account in a listed region
- **WHEN** a linked account in a listed region makes a streamed Chat Completions call, and `ai.unmeasured_calls` is `cutoff`
- **THEN** the account isn't cut off, and the call's tokens are charged from the metrics gap

#### Scenario: A region not listed
- **WHEN** an account makes the same call in a region the stack doesn't list, and `ai.unmeasured_calls` is `cutoff`
- **THEN** the account is cut off with `unmeasured_call`, as before

#### Scenario: An account not linked
- **WHEN** an account not linked to the sink makes the same call in a listed region, and `ai.unmeasured_calls` is `cutoff`
- **THEN** the account is cut off with `unmeasured_call`
