# Proposal

## Why

Desktop GUI users find parts of the app hard to read or unfinished. The app icon looks rough, the Settings "Add source" button does not match the rest of the design, and the install dialog shows a raw plan full of configuration and lock terms with no sign that work is in progress. Users click Sync before Update, nothing tells them that updates exist, and the fixed-width sidebar and inspector can't be adjusted.

## What Changes

- A new app icon replaces the current icon in the macOS bundle and every Linux hicolor size.
- The Settings "Add source" button uses the design system's secondary button style.
- The resource action dialog has no header icon, describes the action in plain language (what will happen and where the resource will be available), keeps the technical plan behind a collapsed "Technical details" disclosure, and shows a spinner while the plan is prepared or the confirmed action is in flight.
- The toolbar shows Update before Sync. After the GUI's final snapshot from launch, an explicit refresh, or its own completed operation or mutation, the session checks the user-personal scope for available updates with the same read-only preview as `agent-marketplace update --dry-run`, and Update shows the number of resources an update would change. No hint shows at zero, before a check completes, or when the check fails.
- The sidebar and inspector are resizable with drag handles that also work from the keyboard, clamp to minimum and maximum widths, and keep their widths in GUI-local storage across launches.

## Capabilities

### New Capabilities

### Modified Capabilities

- `gui-shell`: toolbar update availability and order, resizable panes, plain-language resource action dialog.

## Impact

Desktop GUI frontend (`gui/src`, `gui/DESIGN.md`), the GUI local API contract (`api/gui-local.yaml`, `internal/localapi`), the session core (`internal/session`), the update preview shared by the CLI (CLI output unchanged), and the icon assets under `packaging/gui`. No CLI or TUI behavior changes.

## Release note

The desktop app has a new icon, shows how many updates are available on the Update button, explains installs in plain language with a progress spinner, and lets you resize the sidebar and details panes by dragging or with the keyboard.
