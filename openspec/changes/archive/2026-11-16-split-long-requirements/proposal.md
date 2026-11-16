# Proposal

## Why

Two requirements grew past OpenSpec's 500-character guideline as later changes added to them, and `openspec validate --strict` warns on both: "Manual prices carry forward until dropped" (`shared-prices`, extended by add-scheduled-price-publish) and "AI changes are classified like other changes" (`guard-config`, extended by add-unknown-model-policy). Each now states several behaviors in one description, which makes them harder to read and to cite.

## What Changes

- Each long requirement keeps its header and every scenario, and its description is cut to one behavior without changing its meaning.
- The behaviors taken out become their own requirements, each with its own scenario: in `shared-prices`, dropping a manual price the Price List covers and keeping one above it; in `guard-config`, the list of AI tightening changes and the list of AI loosening changes.
- No behavior changes. Nothing in code, docs, or tests changes.

## Capabilities

### New Capabilities

### Modified Capabilities

- `shared-prices`: "Manual prices carry forward until dropped" is split into three requirements.
- `guard-config`: "AI changes are classified like other changes" is split into three requirements.

## Impact

Specs only. `openspec validate --specs --strict` passes afterwards.
