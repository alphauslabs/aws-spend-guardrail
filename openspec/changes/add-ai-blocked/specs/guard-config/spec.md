## ADDED Requirements

### Requirement: The AI section can block use the guard can't count
`ai.defaults` and each AI scope SHALL accept `blocked`, a list of `agents`, `knowledge_bases`, `bidirectional_streams`, and `async_invoke`. A scope's list SHALL replace the defaults' list, so an empty list blocks nothing. Validation SHALL refuse any other value and a value listed twice. Unset everywhere, nothing SHALL be blocked.

#### Scenario: Blocked by default, allowed for one account
- **WHEN** `ai.defaults.blocked` lists `agents` and `async_invoke`, and the scope of account 111122223333 sets `blocked` to `[]`
- **THEN** the other AI scopes block agents and async invokes, and account 111122223333 blocks nothing

#### Scenario: An unknown kind
- **WHEN** a scope's `blocked` lists `rerank`
- **THEN** validation fails naming the field and the four values it accepts

### Requirement: Blocking applies at once; allowing needs approval
Adding a kind to the blocked list that applies to an account SHALL be tightening and apply at once. Removing one SHALL be loosening and need the approval and delay, and the approval SHALL say, for each account, which use becomes allowed and that it isn't counted toward caps.

#### Scenario: Blocking async invokes
- **WHEN** a version adds `async_invoke` to `ai.defaults.blocked`
- **THEN** the guard applies it on its next pull, with no approval

#### Scenario: Allowing agents for one account
- **WHEN** a version sets the scope of account 730335604511, named `e2enormal`, to `blocked: []` where it blocked `agents`
- **THEN** it waits for approval, and the approval reads that agents become allowed on account 730335604511 (e2enormal) and aren't counted toward its caps
