# Design

## Context

Alerts go through `Guard.alert` (`internal/guard/guard.go`), `Engine.Alert` (`internal/activity`), and once directly through `Alerts.Alert` in `guard/main.go`. All end at `awsx.Alerts.Alert`, which publishes subject and body to the SNS topic, and SNS emails the body as plain text. Bodies are built inline at each call site with `fmt.Sprintf`, often around `err.Error()` or `Target.String()` (`account:<id>`).

## Goals / Non-Goals

**Goals:**
- Readable bodies for every alert, with error detail kept but set apart.
- A check that keeps new alerts in this style.

**Non-Goals:**
- Changing subjects, event details, or which alerts are sent.
- Templating or localizing alerts.

## Decisions

### D1. Helpers for the recurring parts
- `targetName(t config.Target) string`: "Account 891376926337" or "OU ou-ab12-cdef3456", for the start of a sentence.
- `liftReason(code string) string`: a sentence per reason code (`budget_raised`, `scope_removed`, `period_start`, `stack_deleted`), with a fallback for an unknown code ("Reason: <code>.").
- `withError(body string, err error) string`: returns `body + "\n\nError: " + err.Error()`, or the body alone when err is nil.

`targetName`, `targetRef` (the same name inside a sentence: "account 891376926337", "OU ou-ab12-cdef3456"), and `liftReason` live in `internal/guard/alerts.go`. `WithError` and the wording check (D3) live in a small package, `internal/alerttext`, which both `internal/guard` and `internal/activity` use. `internal/activity` can't import `internal/guard` without a cycle, so this avoids two copies.

### D2. Wording per alert
Each of the 14 bodies is rewritten at its call site with the helpers. Facts stay the same: values, ids, versions, times, and the next step for the reader. Durations are printed rounded (`1h5m`) and times in RFC 3339, as now.

### D3. The check lives in the test fakes
The `alerts` fake in `internal/guard` and the engine's test alerter in `internal/activity` check every body they receive, and fail the test when the first line doesn't start with a capital letter, the last sentence line doesn't end with `.`, a line starts with `account:` or `ou:`, or a line other than the last starts with `Error: `. Every existing test that sends an alert then checks it, without separate per-alert tests. A short test also covers alerts the suites don't otherwise reach (code hash unknown, AI guard not started).

*Alternative:* enforce the rules in `awsx.Alerts.Alert` at runtime. Rejected, because rewriting text on the way out hides mistakes, and refusing to send would drop an alert.

## Risks / Trade-offs

- [Email filters on body text break] → Subjects are unchanged; the changelog says so.
- [A few alerts aren't reached by any test] → Covered by the short test in D3, and listed in tasks so none is missed.
