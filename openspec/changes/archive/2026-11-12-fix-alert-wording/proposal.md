## Why

Local alerts are emails a person reads, often during an incident, but many of their bodies read like log lines. They start lowercase, start with a raw target id (`account:891376926337 is frozen`), end without a period, use internal codes (`was lifted (budget_raised)`), or open with a raw Go error chain (`cannot read config from Jennah: read latest config version: rpc error: ...`). The e2e freeze, drift, and outage tests surfaced this. Of the guard's 25 alerts, 14 have one of these problems.

## What Changes

- Every alert body is written as full sentences: it starts with a capital letter and ends with a period.
- Targets are named for people ("Account 891376926337", "OU ou-ab12-cdef3456"), not as `account:<id>`. Events keep the raw form.
- Lift reasons are plain words ("Its ceiling was raised above its spend."), not codes like `budget_raised`. Events keep the codes.
- Raw error text never sits inside a sentence. An alert that carries one ends with a separate `Error: <detail>` line after its sentences.
- Subjects are unchanged: short `guardrail: ...` labels.
- Tests check every alert the guard and the AI guard send in their test suites against these rules.

The 14 alerts reworded: spend frozen, both drift alerts, freeze lifted, config not refreshed, config still not refreshed, config refresh recovered, config version rejected, reconcile incomplete, event not queued, code hash unknown, shared prices not read, could not cut off (AI), and AI guard not started.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `spend-freeze`: local alerts are written as full sentences for a person, with machine detail kept apart.

## Impact

- `internal/guard` (`freeze.go`, `reconcile.go`, `pull.go`, `guard.go`, `prices.go`, `spend.go`), `internal/activity/engine.go`, and `guard/main.go`.
- Test alert fakes in `internal/guard` and `internal/activity` gain the wording check; tests matching old text are updated.
- `CHANGELOG.md` (Changed). No change to events, the stack, or IAM. Anyone filtering alert emails on body text needs to update their filters; subjects are unchanged.
