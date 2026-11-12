## ADDED Requirements

### Requirement: Local alerts are written for a person
Every local alert body SHALL be written as one or more full sentences, starting with a capital letter and ending with a period. A target SHALL be named as "Account <id>" or "OU <id>", not in the `<kind>:<id>` form events use, and a reason SHALL be given in words, not as an internal code. Alert subjects SHALL stay short labels starting with `guardrail: `.

#### Scenario: A freeze alert
- **WHEN** account 891376926337 is frozen at 5.74 USD against a 5.00 USD ceiling with a 100% threshold
- **THEN** the body reads "Account 891376926337 is frozen: its actual cost of 5.74 USD reached 100% of its 5.00 USD ceiling (freeze style B). New spend is blocked, and nothing running was stopped."

#### Scenario: A lift alert
- **WHEN** a freeze is lifted because an approved raise put the ceiling above spend
- **THEN** the body names the account and says in words that its ceiling was raised above its spend, ending with a period, and contains no `budget_raised` code

### Requirement: Error detail in an alert is set apart
Error text from AWS, Jennah, or the guard SHALL NOT appear inside a sentence of an alert body. When an alert includes it, it SHALL follow the sentences on its own last line, starting with `Error: `.

#### Scenario: An alert carrying an error
- **WHEN** config can't be read because Jennah's name doesn't resolve
- **THEN** the body's first lines are sentences saying the last valid config stays in force and when the next reminder comes, and the last line is `Error: ` followed by the error text
