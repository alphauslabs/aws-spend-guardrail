## ADDED Requirements

### Requirement: A cutoff detached outside the guard is noticed
When a recorded cutoff's account no longer has the pack's deny policy attached, the guard SHALL, within a few minutes, record the cutoff as lifted with the reason "detached outside the guard", write a lift event and a drift event, alert once, and watch the account again, cutting it off again only on a new breach. When the pack's deny policy is attached to an account the guard has no cutoff for, the guard SHALL report it once a day with a drift event and an alert, and SHALL NOT detach it.

#### Scenario: An emergency lift by hand
- **WHEN** an administrator detaches the AI cutoff policy from account 730335604511, which the guard cut off at 15:08
- **THEN** within a few minutes the guard records that cutoff as lifted, detached outside the guard, writes a `cutoff_lifted` and a `drift` event, and alerts once

#### Scenario: Watched again
- **WHEN** the account later makes calls that pass its hourly cap
- **THEN** it is cut off again with a new cutoff id

#### Scenario: A policy attached by hand
- **WHEN** the AI cutoff policy is attached to an account the guard has no cutoff for
- **THEN** the daily run alerts once that the policy is attached without a cutoff, and leaves it attached
