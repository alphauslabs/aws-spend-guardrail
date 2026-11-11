# Spec Delta

## ADDED Requirements

### Requirement: Loosening approvals show the reason given
An approval the guard raises for a loosening SHALL show the config version's note in its summary as the reason given, and SHALL carry it in its payload. When the version has no note, the summary SHALL say that no reason was given. A note longer than 4,000 characters SHALL be shortened in the summary, saying it was shortened and naming the version that holds the full note. The `loosening_raised` event SHALL record the note.

#### Scenario: A raise with evidence
- **WHEN** version 7 raises a ceiling and its note reads "Month-end batch, on trend for 6 months; forecast reaches the ceiling tomorrow"
- **THEN** the approval's summary shows that text as the reason given, its payload carries it, and the `loosening_raised` event records it

#### Scenario: No reason given
- **WHEN** version 8 removes a scope and has no note
- **THEN** the approval's summary says no reason was given

#### Scenario: A very long note
- **WHEN** a version's note is 10,000 characters
- **THEN** the summary shows the first part of it, says it was shortened, and names the version holding the full note, and the approval is created
