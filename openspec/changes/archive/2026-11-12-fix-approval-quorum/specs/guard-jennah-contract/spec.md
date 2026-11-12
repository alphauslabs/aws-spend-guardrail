## ADDED Requirements

### Requirement: Loosening approvals carry a quorum rule Jennah accepts
An approval the guard raises for a loosening SHALL state a quorum rule. When the stack requires one approval, the rule SHALL be "any one approver". When the stack requires as many approvals as it lists approvers, the rule SHALL be "all approvers", and the required count SHALL equal the number of approvers. Approvers SHALL be counted once each, ignoring blanks.

#### Scenario: One approval required
- **WHEN** the stack lists one or more approvers and requires 1 approval
- **THEN** the approval is raised with the rule "any one approver", and Jennah accepts it

#### Scenario: Every approver required
- **WHEN** the stack lists two approvers and requires 2 approvals
- **THEN** the approval is raised with the rule "all approvers" and a required count of 2, and Jennah accepts it

### Requirement: An unsupported approval count is a setup error
When the stack's required approvals is neither 1 nor the number of approvers, the guard SHALL NOT send an approval request. It SHALL report a setup error naming the required count, the number of approvers, and the two values that work, through the same alert and reminders as any other failure to apply a config version. The stricter config SHALL stay in force, and protection that needs no approval SHALL keep working.

#### Scenario: Two of three approvers
- **WHEN** the stack lists three approvers, requires 2 approvals, and a published version loosens a ceiling
- **THEN** no approval is raised, the alert says 2 of 3 is not supported and that the value must be 1 or 3, the previous ceiling stays in force, and any tightening in that version applies

#### Scenario: More approvals than approvers
- **WHEN** the stack lists one approver and requires 2 approvals, and a version loosens anything
- **THEN** no approval is raised, and the alert says the value must be 1
