# Proposal

## Why

Desktop GUI users reported small but visible defects. macOS shows the app as `agent-marketplace-gui`. A long load-failure warning makes the resource list scroll sideways and covers the source column. Clicking a resource leaves a folded inspector folded. A disabled resource can show the green "on" dot. Marketplace sources in the sidebar are labeled by a URL subdomain instead of their configured name, and the source tooltip is hard to read in dark mode. Add-source errors appear at the top of Settings instead of beside the form. Diagnostics describes Marketplace discovery with jargon ("fresh", "truncated").

## What Changes

- The macOS app and menu name and the window title read "Agent Marketplace" for the packaged app and the unbundled binary.
- Resource rows truncate long status warnings inside the row, so the list never scrolls horizontally and the warning never covers the source column.
- Clicking a resource row unfolds a folded inspector.
- The row check dot of a disabled resource uses the warning role; green stays reserved for resources that are on.
- Sidebar Marketplace sources are labeled by their configured source name, falling back to the host; the source tooltip meets 4.5:1 contrast in both themes.
- Add-source validation and request errors render inside the Add source section and clear after the next successful add.
- Diagnostics states in plain words whether discovery results are current and whether only part of the catalog is shown.

## Capabilities

### New Capabilities

### Modified Capabilities

- `gui-shell`: row layout, row-click inspector reveal, disabled row indicator, sidebar source labels and tooltip, plain-language discovery health.
- `gui-configuration`: add-source errors stay inside the Add source section.
- `gui-distribution`: the native app name is "Agent Marketplace".

## Impact

Desktop GUI frontend (`gui/src`), the GUI local API contract (`api/gui-local.yaml`, `internal/localapi`), the source description projection in `internal/config`, the native host (`cmd/agent-marketplace-gui`), and macOS packaging metadata. No CLI or TUI behavior changes.

## Release note

The desktop app is now named "Agent Marketplace" in the macOS menu bar, labels Marketplace sources by their configured name, keeps add-source errors next to the form, and explains Marketplace discovery status in plain words. Long warnings no longer make the resource list scroll sideways, and clicking a resource opens a folded details pane.
