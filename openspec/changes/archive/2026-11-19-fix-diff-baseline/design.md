# Design

## Context

The guard (`internal/guard/pull.go`, `evaluate`) reads only the newest published version on each pull and compares it with `st.Effective`: the applied version's config, or, while a loosening waits, `config.Stricter(applied, waiting)`, the applied config with the waiting version's tightening parts. A newer version supersedes a waiting one and is compared with that same `st.Effective`.

The CLI (`internal/cli/cli.go`, `baseline`) compares with the applied version it learns from the newest `config_applied` event in the inbox. That event reaches Jennah through the guard's outbox, so it can trail the guard by a minute or more, and the CLI ignores a waiting loosening altogether.

## Goals / Non-Goals

**Goals:**
- `diff` and `publish` classify a document as the guard will, in the common cases: right after a tightening version was applied, and while a loosening waits.
- Say plainly what the comparison is against.

**Non-Goals:**
- Reading the guard's state directly; the CLI only has Jennah access.
- Two or more versions published between pulls: the guard sees only the newest, and the CLI's answer can differ when an intermediate version was never seen. Rare, and the guard's classification is the one that counts; the approval request shows it.

## Decisions

### D1: The baseline is the config in force once the guard has read the newest version

With A the applied version (from the inbox; 0 if none) and N the newest published version:

1. No A: the baseline is N, as today.
2. N is A, or N doesn't parse or validate (the guard rejects it and keeps A): the baseline is A.
3. `config.Diff(A, N)` has no loosening: the guard applies N in full, so the baseline is N.
4. Otherwise N's loosening waits (or was approved and is waiting for the delay, which the CLI can't tell apart): the baseline is `config.Stricter(A, N)`, as the guard holds while it waits.

Case 4 can be wrong after N's loosening was approved and applied but its report hasn't reached the inbox: the CLI then compares with the stricter merge, so a change may show as loosening that the guard sees as unchanged. That errs toward asking for an approval the guard won't need, never toward hiding one.

### D2: The "compared with" line names the case

- Case 2: `compared with: version 27`.
- Case 3: `compared with: version 28 (published after the applied version 27; it only tightens, so the guard applies it on its next pull)`.
- Case 4: `compared with: version 27 with the tightening parts of version 28, whose loosening waits for approval`.
- Case 1: `compared with: version 28 (no applied version reported yet)`.

## Risks / Trade-offs

- An extra version read per `diff`: negligible.
- The CLI now parses version N with the payer id like the guard does, so a document the guard would reject is detected the same way.
