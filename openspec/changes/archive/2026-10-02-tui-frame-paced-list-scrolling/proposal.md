## Why

Rapid mouse-wheel input can leave the resource list traversing old events after the user stops scrolling. The TUI constructs a complete view for every event even though the terminal renderer displays frames at a lower rate.

## What Changes

- Pace resource-list view construction at 60 Hz while applying every accepted wheel event in its original order.
- Render the final selection after a burst ends, refresh immediately for other input and state changes, and reject stale repaint timers.
- Integrate pacing into the normal TUI program without changing other panes, terminal restoration, editor handoff, or sync-and-quit error reporting.
- Retain ordered-input regression tests, full-path performance evidence, and a real-terminal wheel-burst journey.
- Reconcile the rendering specification with the existing cell-motion mouse-reporting contract and update the TUI reference.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-mouse-navigation`: Resource-list wheel input retains ordered selection while view construction is frame-paced, including trailing repaint and immediate non-wheel updates.
- `tui-rendering`: Frame-paced runtime integration preserves lifecycle behavior and uses the existing mouse-reporting contract rather than prohibiting mouse tracking.

## Impact

The change affects `internal/tui` program construction and rendering, its tests and benchmarks, `tools/tui-qa`, and existing TUI documentation. It adds no dependencies and changes no configuration, inventory, materialization, or scroll-distance policy. The file preview, bundle tree, overlays, and modals keep their existing wheel behavior.
