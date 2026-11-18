# Proposal

## Why

`guardrail diff` and `publish` compare a document with the version the guard last reported applying, read from the newest `config_applied` event in the inbox. The guard compares it with something else: the config in force once it has read the newest published version, which is that version if it only tightens, or the stricter merge of the applied and newest versions while a loosening waits. The two differ whenever a version was published and the report hasn't reached the inbox yet (it travels through the guard's outbox), and whenever a loosening is waiting.

In the add-ai-blocked live check on 2026-11-18, version 28 (tightening) had been applied a minute before, but its report hadn't arrived; `diff` compared version 29 with version 27 and called removing a blocked kind a tightening. The guard compared it with version 28, correctly raised a loosening approval, and kept the block. An operator reading the CLI would have expected the change to apply at once.

## What Changes

- The CLI's baseline becomes what the guard will hold once it has read the newest published version: the applied version when it is the newest (or the newest is invalid); the newest when it only tightens relative to the applied version; and the stricter merge of the two when the newest loosens, as the guard keeps while that loosening waits.
- The "compared with" line says which: "version 28", "version 28 (published after the applied version 27; it only tightens, so the guard applies it on its next pull)", or "version 27 with the tightening parts of version 28, whose loosening waits for approval".
- With no applied version reported, the baseline stays the newest published version, as today.

## Capabilities

### New Capabilities

### Modified Capabilities

- `guard-config`: the CLI shows differences from the config the guard will compare with, not from the last reported applied version.

## Impact

- `internal/cli/cli.go` (`baseline`, the "compared with" line) and its tests.
- `docs/config.md` or `operating.md` where `diff` is described.
- No change to the guard, the config format, or what is published. A CHANGELOG `Fixed` entry; no upgrade steps beyond updating the CLI.
