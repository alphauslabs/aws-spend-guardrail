## ADDED Requirements

### Requirement: A stay_frozen freeze is kept when AWS resets its action at a new period
When a budget period starts for a scope the guard froze whose config is `stay_frozen`, and the scope's freeze policy is no longer attached because AWS reset the budget's executed action (the action reads as in standby), the guard SHALL attach the policy again within one pull and SHALL write a `freeze_carried` event saying AWS reset the action at a new period. If the reset is still in progress, a later run SHALL finish it.

#### Scenario: AWS resets the action on the first of the month
- **WHEN** account 891376926337 has been frozen since 2026-11-13 with `stay_frozen`, and on 2026-12-01 AWS resets its budget's action and detaches the freeze policy
- **THEN** on its next pull the guard attaches the policy again and records `freeze_carried`, and the scope stays recorded as frozen since 2026-11-13

#### Scenario: AWS keeps the action executed
- **WHEN** a new period starts and the freeze policy stays attached
- **THEN** the guard does nothing for that scope

### Requirement: Drift doesn't mistake an AWS reset for a person detaching a freeze
The daily drift check SHALL NOT alert about, or clear the record of, a frozen scope whose policy is detached because AWS reset its action at a new period, or whose carry is still pending. A policy detached while the budget's action still reads as executed SHALL be treated as detached outside the guard, as before.

#### Scenario: A person detaches a freeze mid-month
- **WHEN** someone detaches the freeze policy from a frozen scope and the budget's action still reads as executed
- **THEN** the drift check alerts that the freeze was detached outside the guard and clears the record, and the guard does not attach it again

#### Scenario: The reset and the daily check coincide
- **WHEN** AWS resets the action at a new period shortly before the daily run
- **THEN** the guard attaches the policy again before the drift check, which then finds it attached and reports nothing
