# Design

## Context

- `approvalRequest` (`internal/guard/pull.go`) builds the title, summary, and payload from `config.Change` values rendered with `Change.String()`, the same lines `guardrail diff`, `config_partial`, `loosening_raised`, and `config_applied` use. It is called once per version, when the guard first sees a loosening (`handleLoosening`), and `Pending.FirstSeen` is set to that same moment.
- The guard knows which scopes are frozen (`State.ListFrozen`) and can read a scope's Cost Explorer spend (`spendFor`, $0.01 a call). It lifts a freeze only for a raised threshold above spend (`budget_raised`), a removed scope (`scope_removed`), a new period with `auto_lift`, or a stack delete.
- Alert bodies follow `internal/alerttext` (full sentences, error detail last). The approval summary is shown by Jennah as plain text in the email and console.

## Goals / Non-Goals

**Goals:**
- An approver can decide from the email alone: what changes, what it does to a freeze, and when it takes effect.

**Non-Goals:**
- Changing `guardrail diff`, event payloads, or the payload's existing fields.
- Jennah's part of the email (opening line, payload rendering): that's Jennah's `improve-approval-email`.

## Decisions

### D1. A sentence per change, beside `String()`
`config.Change` gains `Sentence()`, used only by approvals. `String()` stays as it is for the CLI and events, whose readers are operators and machines. Sentences by kind:
- ceiling: "Raise the ceiling of account X from 5.30 USD to 7.00 USD."
- threshold: "Raise the freeze threshold of account X from 90% to 100% of its ceiling."
- mode: "Switch account X from enforce to dry-run, so it is watched but not frozen."
- freeze style: "Switch account X from freeze style B to the narrower style A."
- month end: "Lift the freeze on account X when a new budget period starts (auto_lift)."
- period: "Change the budget period of account X from monthly to quarterly."
- scope removed: "Stop protecting account X."
- alert recipient: "Add ops@example.com as an alert recipient." / "Stop sending alerts to ops@example.com."
- AI fields: one sentence per AI field kind (caps in USD, allowed models and regions, API-key callers, mode, lifts), with a generic fallback "Change the AI guard setting <field> from A to B." for a field without its own wording.

"account X" or "OU X" comes from the existing `targetRef` style.

### D2. Titles by kind
One kind of loosening gets its own title: "Raise a spend cap", "Stop protecting a scope", "Switch a scope to dry-run", "Change alert recipients", "Loosen the AI guard". Several kinds get "Loosen spend protection". Every title ends "for payer <id> (config version <n>)".

### D3. Freeze lines follow the guard's lift rules
For each frozen scope the loosening touches, the summary adds one sentence after the change list. The "would lift" verdict uses the same rules the guard applies after approval: a raised threshold lifts when the new threshold (ceiling times threshold percent) is above spend; a removed scope lifts; anything else says it doesn't lift the freeze by itself. Spend comes from `spendFor`, once per such scope per approval raised. On a Cost Explorer error the line says the spend couldn't be read and gives no verdict, and the approval is still raised: failing to describe a change must not block it.

### D4. Times from `FirstSeen`
The approval is raised in the same run that sets `FirstSeen`, so the summary uses that run's time: earliest apply = now + `LooseningDelay`, expiry = now + `LooseningWindow`, both as "13:25 UTC on 2026-11-12". The delay is written in words ("30 minutes", "1 hour", "1 hour 30 minutes"). The payload carries both as RFC 3339 strings.

## Risks / Trade-offs

- [A Cost Explorer read per frozen scope per approval] → Only frozen scopes the change touches, once per version: cents a month.
- [Automation matching the old title] → Titles are for people; the request key (`guardrail:<payer>:<msp>:v<n>`) is unchanged and is what code should match. The guard's own test that matches the title is updated.
