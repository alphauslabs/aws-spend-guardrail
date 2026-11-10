# Config

Each payer org has one config: a series of numbered, immutable JSON documents (versions) stored in Jennah. The guard applies the newest valid version, with the rules below for anything that loosens protection.

The schema is [`config/schema.json`](../config/schema.json), also printed by `guardrail schema`. Examples are in [`config/examples/`](../config/examples/).

## Document

```json
{
  "payer_account_id": "123456789012",
  "defaults": { "mode": "enforce", "threshold_pct": 90 },
  "scopes": [
    { "account_id": "111122223333", "ceiling_usd": 20000 },
    { "ou_id": "ou-ab12-cd34ef56", "ceiling_usd": 50000, "mode": "dry-run" }
  ]
}
```

| Field | Required | Description |
|---|---|---|
| `payer_account_id` | yes | The payer (management) account the document is for. The guard refuses a document for another account. |
| `defaults` | no | Values every scope takes unless it sets its own. Same fields as a scope's settings below. |
| `scopes` | yes | The accounts and OUs protected by a budget. May be empty. |

### Scope

| Field | Required | Default | Description |
|---|---|---|---|
| `account_id` | one of these two | | A 12-digit member account. |
| `ou_id` | one of these two | | An OU (`ou-...`). Its budget covers every active account in the OU and the OUs below it, and the freeze attaches to the OU. Membership is re-read on every reconcile and at least daily. |
| `ceiling_usd` | yes | | Budget limit in USD for the period. Must be greater than 0. |
| `period` | no | `monthly` | `monthly`, `quarterly` or `annually`. |
| `threshold_pct` | no | `100` | Percentage of the ceiling at which the freeze fires, 1 to 100. Budgets lags spend by hours, so 90 is a sensible value for `enforce`. |
| `freeze_style` | no | `B` | `A` denies a list of actions that create cost. `B` denies everything except reads and actions that reduce cost (stop, terminate, delete) of about 35 common services, and support, billing and security tooling. A service not on its list is blocked entirely while frozen, reads included. |
| `mode` | no | `dry-run` | `dry-run` records `would_freeze` and attaches nothing. `enforce` attaches the freeze policy. |
| `month_end` | no | `stay_frozen` | What happens to a frozen scope when a new period starts: `auto_lift` lifts it, `stay_frozen` keeps it frozen until its ceiling is raised. |

A value set on the scope beats `defaults`, which beat the built-in defaults above.

### Refused documents

A document is invalid, and the guard keeps the last valid config, when it:

- fails the schema (unknown fields, wrong types, values out of range);
- is for another payer account;
- names a scope twice;
- names the payer account or the organization root (`r-...`) as a scope;
- contains an `approvers` field. Who approves a loosening is set in the stack, not in Jennah, so that someone who can write config can't choose who approves it.

## Tightening and loosening

The guard compares each new version with the config in force, field by field:

| Change | Direction |
|---|---|
| Scope added | tightening |
| Lower `ceiling_usd` or `threshold_pct` | tightening |
| `dry-run` to `enforce` | tightening |
| Freeze style `A` to `B` | tightening |
| `auto_lift` to `stay_frozen` | tightening |
| Scope removed | loosening |
| Higher `ceiling_usd` or `threshold_pct` | loosening |
| `enforce` to `dry-run`, `B` to `A`, `stay_frozen` to `auto_lift` | loosening |
| Any change of `period` | loosening |

- A version that only tightens applies on the guard's next pull, within about 5 minutes.
- A version that loosens anything applies its tightening parts at once. For the rest, the guard raises one Jennah approval to the approvers set in the stack. The loosening applies only once the approval is approved **and** the stack's delay has passed since the guard first saw the version.
- If the approval is rejected, expires or is cancelled, the stricter config stays in force.
- A newer version supersedes a pending one, and the pending approval is cancelled.

Raising a frozen scope's ceiling is a loosening. Once applied, the freeze lifts if actual spend is below the new threshold.
