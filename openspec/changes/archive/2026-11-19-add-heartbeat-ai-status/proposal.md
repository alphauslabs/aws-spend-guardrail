# Proposal

## Why

The heartbeat is the guard's state for readers (the Jennah console, `jnh` queries, a future managed agent). Its `ai` section gives the AI guard's mode, cutoffs, and trail status, but not what v0.2.4 to v0.2.8 added: which use is blocked for which accounts (add-ai-blocked), and whether mantle usage is watched (add-mantle-usage). A reader can only piece them together from `ai_blocked` and `mantle_unlinked` events and the guard's alerts, and a payer page can't say "agents blocked on these accounts" or "mantle not watched in us-west-2".

## What Changes

- The heartbeat's `ai` section gains:
  - `blocked`: the accounts and kinds the block policy denies, whether it is attached, and the failure the guard is retrying, if any.
  - `mantle`: each watched region with whether its usage can be read (and why not), and the accounts not linked in it.
  - `unknown_models`: `cutoff` or `count`.
- A change to any of these writes a heartbeat (`reason: "change"`), as other state changes do.
- `state_layout` stays 1: the fields are additions, and readers of layout 1 read defensively.
- In the Jennah console, the payer page's AI section shows the blocked kinds per account, each mantle region's status, the accounts not linked, and how unpriced models are treated.

## Capabilities

### New Capabilities

### Modified Capabilities

- `guard-jennah-contract`: the heartbeat's AI state carries blocks, mantle status, and the unknown-model policy, and changes to them write a heartbeat.

## Impact

- aws-spend-guardrail: `internal/guard/heartbeat.go` (the AI snapshot, from the block policy's and mantle reader's stored state, and the daily mantle link check's reported accounts), tests, `docs/operating.md` ("The guard's state"), CHANGELOG. No stack change.
- jennah: `app/src/lib/guardrail.ts` (reading the new fields), the payer page, tests; an `app-v*` release.
