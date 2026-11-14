# Design

## Context

See proposal.md for why. Today every local alert goes through `awsx.Alerts.Alert` (internal/awsx/aws.go), which prefixes the subject with `[<payer id>]`, cuts it to 100 characters (SNS's limit), and publishes to the alert topic. The budget guard calls it through `Guard.alert`; the AI engine's `Alert` hook is set to `Guard.alert` in `Guard.Trail`; `guard/main.go` calls it directly when the AI guard can't start. Accounts appear in alerts as bare ids ("Account 891376926337 is frozen"), in subjects ("guardrail: 730335604511 cut off"), and inside ARNs, cutoff ids (`ai:730335604511:20261114T1457`), and AWS error text. `awsx.Org.ListAccounts` returns every account's id, name, and state; the guard role has `organizations:ListAccounts`.

## Goals / Non-Goals

**Goals:** a person reading any alert can tell which account it is about without the console; no alert is delayed or lost because names can't be read.

**Non-Goals:**
- Names in inbox events. Events stay keyed by id; readers that want names can list them, and an account rename would otherwise make old events disagree with new ones.
- Names of OUs. OU ids appear in fewer alerts and need a call per OU; left for later if asked.
- Rewriting each alert's wording.

## Decisions

**D1. Names are added at the sink.** `awsx.Alerts` gains an optional `Names` source (`func(ctx) map[string]string`). `Alert` applies `alerttext.NameAccounts(text, names)` to the subject and the body before the prefix is added and the subject is cut. One place covers every alert, present and future. Alternative: pass names into each alert site; rejected because it touches about 35 call sites and a new alert could forget.

**D2. What is named.** `NameAccounts` names a 12-digit run only when it is a known account id, is not part of a longer run of digits or letters, and is not directly inside an ARN or id: the character before it is not `:`, `/`, `-`, `_`, `#`, or `.`, and the character after it is not `:` or `/`. An id already followed by ` (` is left alone, so applying it twice changes nothing. The `Error:` line (from `alerttext.WithError`) is left exactly as it is, so AWS error text stays verbatim. An account whose name is empty is left bare. The payer prefix `[<payer id>]` is added after naming and stays bare.

**D3. The subject stays within 100 characters.** If the named subject plus the prefix is longer than 100 characters, the subject is sent with bare ids instead (and still cut as today if even that is too long). The body is always named.

**D4. Names are cached per instance, never block.** The source lists accounts with a 3-second timeout on first use and when the cached list is older than an hour, and keeps the last good list when a refresh fails. With no list yet (first call failed), the alert is sent with bare ids at once; the next alert tries again. Names are labels only: the guard never acts on a name.

## Risks / Trade-offs

- Account names are chosen by the organization and can be renamed; an alert shows the name as of the last listing (up to an hour old). Acceptable for a label.
- AWS doesn't require unique names; two accounts with the same name still show different ids.
- A large organization (1,000 accounts) needs about 50 `ListAccounts` pages per refresh, at most once an hour per warm instance, well within limits.
- Names may say more about a client than ids do; they go only to the config's alert recipients, the same people who manage the org.
