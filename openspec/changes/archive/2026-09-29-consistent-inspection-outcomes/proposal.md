## Why

The status, sync check, and terminal UI inspection paths could disagree about the severity and exit status of the same materialization issue.

## What Changes

- Classify inspection failures and warnings consistently across status, sync check, and the terminal UI.
- Show warning-only outcomes without turning them into failures, while surfacing unsafe aggregate failures as errors.

## Capabilities

### New Capabilities

- `inspection-outcomes`: Health and diagnostics of read-only materialization inspection.

### Modified Capabilities

None.

## Impact

CLI status and sync check, terminal UI diagnostics, and reporting of materialization issues.
