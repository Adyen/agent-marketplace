# Clean up workspace chrome

## Why

The workspace toolbar duplicates the refresh shortcut, and several visual details make resource navigation and diagnostics harder to read or copy.

## What Changes

- Keep explicit refresh on Command-R without a visible refresh control; preserve focus-triggered reload and reload after mutations.
- Fix grouped-list header coverage, simplify status and inspector details, lighten modal overlays, and show navigation state and pending work clearly.
- Let users select diagnostic text and copy every diagnostic message.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `gui-shell`: make refresh the exception to visible action controls while retaining keyboard access and ordinary reload behavior.

## Impact

The Vue desktop frontend, its component and browser tests, design guidance, and embedded frontend assets change. Local API, native host lifecycle, configuration formats, and Space-key behavior do not change.

## Release note

The desktop workspace has clearer navigation and readable, copyable diagnostics. Refresh remains available through Command-R and when the window regains focus.
