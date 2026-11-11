# Design

## Context

- The inbox has one table, `events`, keyed by `event_id`, with `occurred_at` a non-null `TIMESTAMP`. Every event kind, including the daily heartbeat, goes there.
- `guardrail setup` (`internal/cli/cli.go`) creates both datasets and declares their tables, but only when `hasTables` finds a declared column missing. A table whose columns all exist is never re-declared.
- Jennah's retention (`TableDeclaration.retention`, in `jennah-sdk-go` v0.78.0) treats an unset retention as "keep what the table has", `days` 1 to 3650 as "set", and `days = 0` as "remove". Setting or shortening one removes existing rows already past the limit, and the declaration's `TableStatus.retention_change.removes_rows` says so. `GetSchema` reports the retention in force, read from Spanner, once applied.

## Goals / Non-Goals

**Goals:**
- Every inbox, new or existing, ends up with a 400-day retention on `occurred_at`, through the setup command operators already run.

**Non-Goals:**
- Per-kind retention, a configurable retention, or splitting heartbeats into their own table.
- Retention on the config dataset.
- Any change to the guard Lambda or its permissions.

## Decisions

### D1. 400 days, one table, fixed in the schema package
`schema.InboxTables()` sets `Retention: {Column: occurred_at, Days: 400}`, from a named constant `InboxRetentionDays`. One retention for all kinds keeps the layout as it is; volume is small (one heartbeat a day per payer org), so heartbeats don't need a shorter one. A fixed value, rather than a setup flag, keeps every payer org alike and the docs simple.

*Alternative:* a separate `heartbeats` table with a short retention. Rejected for now: it is a layout change for little storage, and the guard would write to two tables.

### D2. No layout version bump
The retention changes no column, and the guard neither reads nor writes it. `LayoutVersion` describes the shape of rows the guard writes, so it stays at 1.

### D3. Setup re-declares when the retention differs
`hasTables` becomes a check that a table needs declaring when a column is missing **or** its reported retention differs from the declared one. A declared retention compared to `GetSchema`'s compares column and days. Re-declaring sends the whole table declaration, which Jennah treats as a no-op for everything that already matches.

Before declaring, setup prints a line for each table whose retention will be set or shortened, for example `inbox events: events older than 400 days will be removed`. After declaring, a `retention_change` with `removes_rows` in the response is what setup reports, so the output reflects what Jennah did, not only what setup intended.

A retention change can still be applying when the declaration returns (`SCHEMA_STATUS_PENDING`). Setup reports that the retention is being applied rather than waiting; a later `setup` run sees it in force and reports nothing changed.

### D4. The fake keeps a declared retention
`jennahtest` stores a table's retention on declare and returns it from `GetSchema`, keeping an unset retention as "keep" and `days = 0` as removal, so CLI tests exercise the same contract as Jennah.

## Risks / Trade-offs

- [An operator who wants events older than 400 days loses them on the next setup] → Setup says so before applying, and the docs say how to export the inbox first (`jnh datasets query ... -o json`).
- [Expired events stay readable for up to about three days] → Documented. Nothing in the guard or the learning agent depends on an event being gone.
- [An older CLI re-runs setup] → It sends no retention, which Jennah treats as "keep", so it never removes one.

## Migration Plan

1. Release the CLI with this change.
2. For each existing payer org, an operator runs `guardrail setup` once; new payer orgs get the retention at creation.

Rollback: re-run setup with a release that declares `days = 0`, or remove it by hand with `jnh datasets schema declare`.
