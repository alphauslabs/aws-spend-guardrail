# Spec Delta

## Purpose

Defines the member guard's config document: which callers it caps and how, the mode it runs in, and which changes tighten at once or loosen only after approval.

## ADDED Requirements

### Requirement: A member config names callers and their caps
A member config SHALL have a `mode` of `enforce` or `dry-run` and a `callers` list. Each entry SHALL name exactly one of `role` (an IAM role name) or `user` (an IAM user name), and SHALL set at least one of `hourly_usd`, `daily_usd`, and `monthly_usd`. A `role` entry MAY set `per_session`.

#### Scenario: A role, a per-session role, and an API key's user
- **WHEN** a config lists `{"role": "app-chatbot", "daily_usd": 20}`, `{"role": "claude-code", "per_session": true, "daily_usd": 15}`, and `{"user": "BedrockAPIKey_ab12", "monthly_usd": 100}`
- **THEN** it validates, and each caller has only the caps it sets

### Requirement: Only named callers are capped
A member config SHALL NOT cap callers it doesn't name. There SHALL be no wildcard or account-wide entry.

#### Scenario: A wildcard name
- **WHEN** an entry sets `role` to `*`
- **THEN** validation fails, saying callers must be named

### Requirement: Member configs are validated
Validation SHALL refuse an unknown field, a `mode` other than `enforce` or `dry-run`, an entry naming both or neither of `role` and `user`, a name that isn't a valid IAM name, a full ARN, `per_session` on a `user` entry, a negative cap, an entry with no cap, a caller named twice, and a lift that isn't a member cutoff id.

#### Scenario: An ARN instead of a name
- **WHEN** an entry sets `role` to `arn:aws:iam::111122223333:role/app-chatbot`
- **THEN** validation fails and says to give the role's name

#### Scenario: Per session on a user
- **WHEN** an entry sets `user` and `per_session`
- **THEN** validation fails naming `per_session`

### Requirement: Tightening applies at once
A new caller entry, a new or lower cap, and a change of `mode` from `dry-run` to `enforce` SHALL apply on the member guard's next config pull, with no approval.

#### Scenario: Adding a caller
- **WHEN** a version adds `{"role": "app-chatbot", "daily_usd": 20}`
- **THEN** the member guard applies it within 15 minutes with no approval

### Requirement: Loosening needs approval
A higher or removed cap, a removed caller entry, `per_session` turned off, a change of `mode` from `enforce` to `dry-run`, and a cutoff id added to `lifts` SHALL apply only after the account's approvers approve the version, and after the stack's delay when one is set. The tightening parts of such a version SHALL apply at once.

#### Scenario: Raising a cap
- **WHEN** a version raises `app-chatbot`'s daily cap from $20 to $40 and adds a new caller
- **THEN** the new caller applies at once, and the raise waits for approval

#### Scenario: Rejected
- **WHEN** the approvers reject a loosening version
- **THEN** its loosening parts never apply, and a rejection event is recorded

### Requirement: Lifts name a member cutoff id
A lift SHALL name a member cutoff id: the account, the UTC minute of the cutoff, and the caller, such as `member:111122223333:20261121T0930:role/app-chatbot` or `member:111122223333:20261121T0930:role/claude-code/alice@example.com`.

#### Scenario: Lifting a session's cutoff
- **WHEN** a config lists `member:111122223333:20261121T0930:role/claude-code/alice@example.com` under `lifts`
- **THEN** it validates
