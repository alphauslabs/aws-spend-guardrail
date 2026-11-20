# Design

## Context

See proposal.md for why. `internal/guard/watch.go` keeps each watch as two reported records keyed by budget name: `spend_watch` (when it started) and `spend_check` (when Cost Explorer was last read). `watchSpend` ends a watch when the budget is gone, at 48 hours (`SpendWatchFor`), at a new period, or when the budget, read at the start of a run after the watch began, reports nonzero spend. Otherwise it reads Cost Explorer at most once an hour (`spendFor`) and freezes, or records `would_freeze`, at the threshold.

## Goals / Non-Goals

**Goals:**
- No window in which a spending scope's budget reads zero and nothing can freeze it.
- No extra Cost Explorer reads for scopes that aren't spending.

**Non-Goals:**
- Changing the first 48 hours, the hourly spacing, or how a freeze is made.
- A new inbox event kind; the alerts are enough to notice and record a slow budget.

## Decisions

### D1. Past 48 hours, the hourly read decides

At 48 hours the watch no longer ends on time alone. Each hourly check past 48 hours reads Cost Explorer as before, and:

- cost of zero: the watch ends (the budget has nothing to catch up on);
- cost above zero: the watch continues, and the threshold check runs as in the first 48 hours;
- no read (Cost Explorer can't be read): the watch continues, and the next hourly check tries again, so a failed read never ends protection.

The other ends (budget reports spend, budget gone, new period) apply as today. A scope that is frozen still skips reads, as today, and its watch stays until one of those ends.

Alternative: keep watching while the budget reads zero, whatever Cost Explorer shows. Rejected: an idle account's budget reads zero for good, so its watch would read Cost Explorer every hour until the period ends, about $7 a month per idle scope.

Alternative: extend the fixed limit (say to 96 hours). Rejected: it only moves the gap.

### D2. One alert when a budget lags, one when it catches up

A reported record `spend_lag`, keyed by budget name, holds when the lag was first alerted. On the first check past 48 hours that finds a cost above zero, with no `spend_lag` record, the guard alerts:

> guardrail: budget for account 111122223333 (client-a) hasn't caught up
> AWS Budgets still reports no spend for this scope 48 hours after the guard changed its budget, while Cost Explorer shows 3.10 USD this month. Until Budgets catches up, its freeze action can't fire, so the guard keeps checking Cost Explorer every hour and freezes the scope itself at its threshold.

When a watch with a `spend_lag` record ends because the budget reports spend, the guard alerts once that it caught up, with the hours since the watch started, then clears the record. Any other end of the watch clears it without an alert. `endSpendWatch` clears all three records.

### D3. Docs

`docs/operating.md` ("When Budgets reports no spend") and the cost line in `docs/install.md` say the watch continues past 48 hours while the scope is spending and its budget reads zero, and name the two alerts. `docs/aws-facts.md` gains a row once a lag past 48 hours is seen (from the alert), not before.

## Risks / Trade-offs

- [A budget that never recalculates, for example an AWS fault] -> The watch continues to the end of the period, one Cost Explorer read an hour ($0.24 a day), and the scope stays protected by the guard. The alert tells the operator to look.
- [Cost Explorer itself lags a few hours] -> Unchanged from today: a scope whose first spend hasn't reached Cost Explorer at 48 hours ends its watch, then relies on the Budgets action. Spend that reaches Cost Explorer later also reaches Budgets about as soon.
