## ADDED Requirements

### Requirement: Preflight counts the payer account's budget actions
`guardrail preflight` SHALL count the budget actions in the payer account on budgets the guard doesn't own, add the document's enforcing scopes, and fail when the total is over 100. Its output SHALL give both counts and the limit. Budget actions on budgets the guard owns SHALL NOT be counted twice: they are already among the document's enforcing scopes or will be removed.

#### Scenario: Other budget actions leave too little room
- **WHEN** the payer account has 30 budget actions on budgets the guard doesn't own and the document has 80 enforcing scopes
- **THEN** preflight fails, saying 30 actions are in use outside the guard, the document needs 80, and AWS allows 100

#### Scenario: Enough room
- **WHEN** the payer account has 5 budget actions outside the guard and the document has 60 enforcing scopes
- **THEN** preflight reports the counts and passes this check

### Requirement: A budget action AWS refuses for the limit is reported as a scope that can't freeze
When AWS refuses a scope's budget action because the payer account is at its limit of 100, the guard's alert SHALL name the scope, say it can't freeze until it has an action, give the limit, and name the remedy: fewer enforcing scopes (OU scopes or `dry-run`) or fewer budget actions outside the guard. The scope's budget SHALL still be written, and later runs SHALL try the action again.

#### Scenario: The 101st action is refused
- **WHEN** the payer account already has 100 budget actions and the effective config adds an enforcing scope for account `111122223333`
- **THEN** the guard's alert says account 111122223333 has no budget action and can't freeze because the payer account is at AWS's limit of 100 budget actions, and names the remedy

### Requirement: A refused budget action is alerted once
The alert for a scope whose action AWS refused for the limit SHALL be sent once while the refusal lasts, not on every run. When the scope gets its action, leaves the config, or leaves `enforce`, the condition SHALL clear, and a later refusal SHALL be alerted again.

#### Scenario: The alert isn't repeated every run
- **WHEN** the same scope's action is refused for the limit on three runs in a row
- **THEN** one alert is sent for it

#### Scenario: A pending loosening briefly needs more actions
- **WHEN** a version that replaces 10 enforcing scopes with 10 others waits for approval, so the stricter config in force holds both sets
- **THEN** the guard reports the scopes it can't create actions for, and once the version is approved and the removed scopes' actions go, it creates the rest on its next run
