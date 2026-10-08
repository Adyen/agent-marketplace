## Why

The TUI errors screen presents diagnostics as text, but users cannot select and copy that text using the existing drag-selection and terminal clipboard interaction. Making the errors body selectable lets users copy the diagnostic text they see while preserving current selection behavior elsewhere.

## What Changes

- Allow drag selection and clipboard copying within the displayed errors body, including wrapped Unicode text, while excluding the footer and terminal presentation controls.
- Define cancellation and exit behavior for error-screen selections, including scrolling, resizing, diagnostic changes, Escape, `e`, and Ctrl+C.
- Keep selection behavior for other overlays, preview selection, mouse reporting, and clipboard transport unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-mouse-navigation`: allow selection in the errors body while retaining the overlay exclusion for other overlays, and specify bounded error-screen selection behavior.

## Impact

Affected areas are TUI errors rendering, selection-region identification and highlighting, native frame-paced mouse-event transport, tests, and the `tui-mouse-navigation` OpenSpec capability. No clipboard dependency or diagnostic, configuration, or filesystem behavior changes are in scope.
