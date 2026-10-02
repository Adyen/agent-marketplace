## Why

Mouse-wheel reporting captures drag events before the terminal or multiplexer can select text. Users need to copy details and preview text while retaining pointer-based scrolling.

## What Changes

- Add unmodified drag selection and copy-on-release to the right-hand details and file-preview panes.
- Keep list, bundle-tree, preview, and overlay wheel routing and resource-list frame pacing unchanged.
- Clear selection on scroll, resize, resource/file changes, and replacement preview content; Escape clears selection before its normal action.
- Leave resource lists, bundle trees, footers, and overlays outside text selection.

## Capabilities

### New Capabilities

None

### Modified Capabilities

- `tui-mouse-navigation`: add right-pane text selection and clipboard delivery without changing wheel navigation.

## Impact

TUI input, rendering, selection state, terminal verification, and published mouse-interaction guidance. No configuration, filesystem policy, dependency, Ghostty, or Herdr changes.
