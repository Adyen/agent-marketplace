## Why

A Sync or Update could run for seconds with no visible sign it had started. Feedback waited for the session to report the running operation, so the window between the press and that report showed nothing, and the toolbar buttons never showed a busy state.

## What Changes

- Pressing Sync or Update shows the matching progress toast and a busy indicator on the pressed button from the moment the request is sent.
- The feedback clears when the operation settles, or immediately if the request fails to start or is refused as busy.

## Capabilities

### Modified Capabilities

- `gui-shell`: Sync and Update feedback starts at the click.

## Impact

Desktop GUI controller, workspace toast and toolbar. No configuration, local API, Marketplace, TUI, or resource behavior changes.

## Release note

Sync and Update now show a spinner on their button and a progress toast as soon as you press them.
