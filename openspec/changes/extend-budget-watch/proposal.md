# Proposal

## Why

After the guard creates or updates a budget, AWS Budgets reads zero until it recalculates, so its freeze action can't fire. The guard covers that window by reading the scope's spend in Cost Explorer once an hour and freezing the scope itself if it's over its threshold, but only for 48 hours. If a budget ever takes longer than that to catch up, a scope that is spending has no freeze until it does. The longest lag measured so far is about 27 hours, so this hasn't happened yet; the change closes the gap before it does.

## What Changes

- At 48 hours, the watch no longer just stops. While the budget still reports zero and Cost Explorer shows the scope has spent something this period, the guard keeps reading Cost Explorer once an hour and freezing on its threshold, as before. The watch still ends when the budget reports spend, the budget is gone, or a new budget period starts.
- A watched scope that Cost Explorer shows with no spend this period stops being watched at 48 hours, as today: its budget has nothing to catch up on, and watching it longer would only add Cost Explorer reads.
- The first time a budget is still at zero after 48 hours while Cost Explorer shows spend, the guard alerts once, naming the scope, how long it has waited, and the Cost Explorer figure. It alerts once more when that budget catches up, with how long it took, so the lag can be recorded.
- The docs (`docs/operating.md`, `docs/install.md`) describe the longer watch and its Cost Explorer cost.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `spend-freeze`: a changed budget is watched past 48 hours while it reads zero and the scope has spent; a budget that hasn't caught up after 48 hours is alerted once, and again when it catches up.

## Impact

- `internal/guard/watch.go` and `internal/guard/watch_test.go`: the end-of-watch rule and the two alerts.
- `docs/operating.md`, `docs/install.md`, CHANGELOG.
- No stack, template, permission, or config change. Cost Explorer reads go up only for scopes whose budget lags past 48 hours while spending: $0.24 a day each.
