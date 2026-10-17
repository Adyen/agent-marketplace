## Why

Operational logging behavior is implemented and tested, but its historical
document still advertises obsolete consumer configuration behavior.

## What Changes

- Baseline current logging configuration, output, fallback, and terminal
  routing without changing runtime behavior.
- Bind the baseline scenarios to executable tests.
- Delete the historical logging document and update its references and
  harvest-ledger row.

## Capabilities

### New Capabilities

- `logging`: operational log configuration and output.

### Modified Capabilities

None.

## Impact

Specification and test traceability only. The current public logging reference
remains the user-facing guide; logging defaults and formats remain unchanged.
