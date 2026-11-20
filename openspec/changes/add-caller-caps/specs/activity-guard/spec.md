## ADDED Requirements

### Requirement: Totals are kept per named caller
For each caller a scope in force names, the guard SHALL keep the caller's estimated spend in each account over the same windows as account totals, counting each call at the time CloudTrail recorded it. A caller's totals SHALL start when its entry first applies: calls the guard read before then SHALL NOT count toward them. Calls from callers no scope names SHALL count toward account totals only.

#### Scenario: A named role
- **WHEN** role `app-chatbot` is named in account 111122223333's scope and its sessions make calls costing $3 at 10:58
- **THEN** the role's hourly, daily, and monthly totals in that account each include the $3, and so do the account's

#### Scenario: A caller added mid-day
- **WHEN** a caller entry applies at 14:00 for a role that spent $10 that morning
- **THEN** the role's daily total includes none of the morning's $10
