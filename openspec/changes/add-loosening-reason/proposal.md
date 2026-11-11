# Proposal

## Why

When spend is legit but heading past a limit, someone has to propose a higher limit and an approver has to decide. Today that someone writes the reason into the config version's note (`guardrail publish --note`), but the approval the guard raises for the loosening shows only the list of changes. The approver sees "ceiling 5000.00 -> 8000.00" with no reason, and has to look up the version to find out why.

This matters more once the Jennah agent proposes raises itself. It publishes a version with the higher limit and its evidence (trend, baseline, why the spend looks legit) in the note. If that evidence isn't in the approval, the agent would have to ask a second time, and the admin would approve twice. With the note in the guard's approval, there is one request, with the reasoning in it.

## What Changes

- **The loosening approval shows the version's note** in its summary under "Reason given", and carries it in the payload. A long note is shortened to fit, saying so and naming the version where the full note is.
- **A version without a note says so** in the approval, so an approver can tell "no reason given" from a missing feature.
- **The `loosening_raised` inbox event records the note,** so the record of who asked for what, and why, is in one place.
- **The docs** tell people publishing a loosening to give a reason with `--note`.

Non-goals: recording who published a version (the config row carries no author today), requiring a note, and any change to how loosening is approved or delayed.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `guard-jennah-contract`: loosening approvals carry the version's reason. The requirement that approvals identify their version lives in `add-budget-freeze-guard`, not archived yet, so this delta adds a requirement rather than modifying that one.

## Impact

- **Code:** `internal/guard/pull.go` (`approvalRequest` and the `loosening_raised` event), the `Pending` state (the note seen with the version).
- **Docs:** `docs/config.md` and `docs/operating.md`, `CHANGELOG.md`.
- **Jennah:** none. Approval summaries can be up to 16,000 characters.
- **Deployed guards:** take effect on the next loosening after an update; pending approvals are unchanged.
