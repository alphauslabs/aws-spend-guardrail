# Spec Delta

## ADDED Requirements

### Requirement: A new install checks coverage on its first run
A new install SHALL run the coverage check in the first run that applies a config version, instead of waiting for the daily run, and SHALL report the unprotected accounts it finds then as it would at a daily check. An applied config with no scopes SHALL count as a config to check against.

#### Scenario: A first install with an empty config
- **WHEN** a guard is installed in an organization with 6 active member accounts, and the latest config version has no scopes and no exclusions
- **THEN** within the first run that applies it, the inbox has an `unprotected_account` event for each of the 5 accounts other than the payer, and one local alert lists them

#### Scenario: The daily run after
- **WHEN** the next daily check finds the same 5 accounts still unprotected
- **THEN** it reports none of them again

#### Scenario: A restart is not a new install
- **WHEN** an installed guard's function restarts with its state intact
- **THEN** no coverage check runs outside the daily run and account events
