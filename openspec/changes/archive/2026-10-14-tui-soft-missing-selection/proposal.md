## Why

A configuration can select a non-required skill that a successfully loaded registry does not provide. The CLI already treats this as a warning, but the TUI marks the whole snapshot's drift as unverifiable: the missing row is counted as unavailable, so the final stage reports `driftFailed`, sync, update, install, and toggle reloads fail with "authoritative state could not be verified", and the footer shows "drift unavailable".

## What Changes

- Rows for non-required selections absent from a loaded registry carry a soft unavailable flag; they still render with their not-found reason and the warning diagnostic.
- Soft rows no longer block drift assessment, footer drift state, or reload acceptance.
- Required or collection selections that are not found, and selections whose registry catalog failed to load, keep failing the assessment.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-startup`: Missing optional selections are warnings that do not fail the authoritative assessment.

## Impact

TUI snapshot assembly (`internal/tui/actions.go`), unavailable-row state (`internal/tui/state.go`), and footer drift state (`internal/tui/footer.go`), with new TUI tests. No configuration schema, resolver, pipeline, or CLI behavior changes.
