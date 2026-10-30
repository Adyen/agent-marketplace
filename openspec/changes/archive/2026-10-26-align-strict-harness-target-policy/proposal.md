## Why

The TUI promises unknown-ID preservation although configuration validation
rejects those IDs before normal loading. Issue #935 selects strict validation:
your CLI must understand every tool named in the configuration.

## What Changes

- Correct save/resize requirements to describe supported harness target IDs.
- Explicitly require visible refusal of unknown IDs rather than warning-only
  subset operation or silently removing entries.
- Replace unreachable model fixtures and bind the existing supported-ID
  terminal outcomes.
- Add real-terminal refusal, unchanged-file and recovery witnesses.

Runtime validation is unchanged. No warning-only compatibility, new harness
support, dependency or product hook is introduced.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-interactions`: supported target selection and unknown-target refusal.

## Impact

TUI specifications, tests and documentation only. ADR 0010 records the accepted
decision and examples. Issues #935 and #928 track implementation until merge.

## Release note

none - Specification, decision-record and test alignment only. Unknown harness
target IDs remain configuration errors; runtime behavior is unchanged.
