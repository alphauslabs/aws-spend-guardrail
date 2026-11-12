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
| `excluded_accounts` | no | Accounts left out of protection on purpose, such as an SSO or shared-services account. The guard never reports them as unprotected. The payer account always counts as excluded. An account can't be both excluded and a scope. |
| `alert_emails` | no | Up to 20 email addresses that receive the guard's local alerts, besides the stack's `AlertEmail`. Adding or removing one needs approval (see below). Each address gets a confirmation email from AWS and receives alerts only after confirming. See [alert recipients](operating.md#alert-recipients). |

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
- has more than 100 scopes in `enforce` once defaults are applied. Each enforcing scope needs a budget action in the payer account, and AWS allows 100 per account. Group accounts into OU scopes, or set some scopes to `dry-run`;
- names the payer account or the organization root (`r-...`) as a scope;
- lists an account both in `excluded_accounts` and as an account scope, or lists an exclusion twice;
- lists an alert recipient that isn't a plain email address (`Ops <ops@example.com>` is refused), lists one twice (case doesn't matter), or lists more than 20;
- contains an `approvers` field. Who approves a loosening is set in the stack, not in Jennah, so that someone who can write config can't choose who approves it.
- has an `ai` section with an unknown field, a negative cap or price, an account or OU named twice, the organization root as a scope, or a malformed region, caller ARN, or cutoff id.

### AI

The optional `ai` section configures the [AI guard](ai-guard.md). It has no effect unless the stack has `EnableAIGuard=true`.

```json
"ai": {
  "defaults": { "monthly_usd": 2000, "mode": "dry-run" },
  "scopes": [
    { "account_id": "111122223333", "hourly_usd": 20, "daily_usd": 200, "mode": "enforce" },
    { "ou_id": "ou-ab12-cd34ef56", "monthly_usd": 500 }
  ],
  "models": ["amazon.nova-micro-v1:0", "anthropic.claude-haiku-5-5"],
  "regions": ["us-east-1", "us-west-2"],
  "api_key_callers": ["arn:aws:iam::111122223333:role/batch-jobs"],
  "prices": { "acme.custom-model-v1:0": { "input_per_1k": 0.001, "output_per_1k": 0.004 } },
  "lifts": ["ai:111122223333:20261111T0930"]
}
```

| Field | Description |
|---|---|
| `defaults` | Caps and `mode` every AI scope takes unless it sets its own. `mode` here also applies to accounts no AI scope covers, for the rules in [AI guard](ai-guard.md#caps-and-rules). Default `dry-run`. |
| `scopes` | Accounts and OUs with caps: `hourly_usd`, `daily_usd`, and `monthly_usd` of estimated spend (0 or absent means no cap), and `mode`. An account scope beats an OU scope. |
| `models` | Models allowed, by model id, inference profile id, or ARN. Absent: any model the price table knows. An empty list allows none. |
| `regions` | Regions allowed for Bedrock calls. Absent: any region. |
| `api_key_callers` | IAM users or roles that may use Bedrock API keys. Absent: nobody. A role covers its sessions. |
| `prices` | Per-model prices in USD per 1,000 tokens, overriding the shipped table or adding a model to it. |
| `lifts` | Cutoff ids to lift. |

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
| Account added to or removed from `excluded_accounts` | notice: applies at once, no approval, since it changes what's reported, not what's protected |
| Address added to or removed from `alert_emails` | needs approval, like a loosening: removing one can silence freeze alerts, and adding one can send them to someone outside the MSP. This holds in a first version too. |
| AI: a lower cap, a scope added, a model, region, or API-key caller removed, a higher price, `dry-run` to `enforce` | tightening |
| AI: a higher or removed cap, a scope removed, a model, region, or API-key caller added, a lower, new, or removed price, `enforce` to `dry-run`, a lift added | loosening |

- A version that only tightens applies on the guard's next pull, within about 5 minutes.
- A version that loosens anything applies its tightening parts at once. For the rest, the guard raises one Jennah approval to the approvers set in the stack. The loosening applies only once the approval is approved **and** the stack's delay has passed since the guard first saw the version. The approval shows the version's note (`guardrail publish --note`) as the reason given, so write why the change is needed there: approvers decide from it.
- If the approval is rejected, expires or is cancelled, the stricter config stays in force.
- When a version's only changes awaiting approval are to `alert_emails`, the approval is titled "Change alert recipients" instead of "Loosen", and lists each address added or removed. Until it's approved, the recipients in force stay as they are.
- A newer version supersedes a pending one, and the pending approval is cancelled.

Raising a frozen scope's ceiling is a loosening. Once applied, the freeze lifts if actual spend is below the new threshold.
