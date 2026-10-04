## Why

Marketplace skills with staged supporting files omit their primary definition from the full-view browser. Skills found through Marketplace discovery cannot be inspected before installation, and long bundle labels can displace the preview from its mouse-selection coordinates.

## What Changes

- Show and initially preview `SKILL.md` alongside verified supporting files for installed Marketplace skills.
- Load exact resource revision inspection projections asynchronously when opening uninstalled Marketplace skills, without installation or configuration changes.
- Keep the full-view preview within its allocated columns so displayed text remains selectable through the terminal clipboard protocol.
- Preserve pointer-targeted scrolling, selection cancellation, and non-preview clipboard boundaries.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-navigation`: Browse installed Marketplace skill definitions and supporting files, and exact verified inspection projections before installation, with visible failure and stale-response handling.
- `tui-mouse-navigation`: Keep full-view preview selection aligned with displayed content even when bundle labels exceed the tree width.

## Impact

TUI bundle construction, full-view rendering, asynchronous detail loading, published TUI interaction documentation, and direct terminal journeys. Reuse the existing Marketplace inspection client. No server API, installation, materialization, configuration schema, acquisition lock, or terminal configuration changes.
