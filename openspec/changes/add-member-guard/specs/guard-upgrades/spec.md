# Spec Delta

## ADDED Requirements

### Requirement: The latest guard release ignores member releases
Wherever the guard CLI compares a payer org's release with the latest release, it SHALL use the newest `vX.Y.Z` release and SHALL ignore `member-v*` releases and any other tag that isn't `vX.Y.Z`.

#### Scenario: A member release is newest
- **WHEN** the newest release overall is `member-v0.1.0` and the newest guard release is `v0.3.0`
- **THEN** `guardrail versions` compares each payer org with `v0.3.0`, and a payer org running `v0.3.0` is shown as current
