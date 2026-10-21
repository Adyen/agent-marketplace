## Why

After the target harnesses change, a still-selected resource is missing from its new target and orphaned in the old one. The TUI keyed drift items by resource and inserted orphaned items last, so the orphaned copy won: the selected resource showed under Broken with `Status: orphaned` until the next sync, as if changing harnesses had broken it (#865). Only the end state after sync was correct.

## What Changes

- One helper builds the drift-by-resource map for both row builders, with precedence broken, then missing, then an orphaned copy.
- A row's orphaned drift item now shows as enabled and pending sync (`◐`, Pending sync section) instead of `◌ orphaned` under Broken. Drift attaches only to selected rows, so an orphaned item on a row is always a stale copy in a harness the resource no longer targets, as when a harness is removed. The unreachable orphaned glyph style is removed, and so is the separate `orphan` row key, so the selection stays on the row when sync clears the stale copy.
- `tui-interactions` specifies that retargeted resources report pending sync.
- A TUI QA journey covers the harness modal end to end: project scope, empty-selection refusal, cancel without writes, saving project harnesses, the pending status before sync, and the move between harness folders on sync.

## Capabilities

### New Capabilities

### Modified Capabilities
- `tui-interactions`: retargeted resources report pending sync.

## Impact

`internal/tui` row drift mapping (`buildRows`, `applyDriftItems`), row status and status grouping (`state.go`, `tree.go`), row identity (`flatitems.go`), and the glyph styles (`theme.go`). Deselected resources have no drift on their rows before or after this change.
