# Proposal

## Why

In the desktop app, the "sync complete" and "update complete" notifications appear only after the user does something else. A native Linux WebKitGTK reproduction showed the local API writing every event promptly. The webview's custom-scheme response stream held back the trailing frames, including the final snapshot and the notification, until another request woke the transport. That happened in 11 of 12 operations, with delays of 3 to 13 seconds. Events sent through the Wails runtime event bridge reached the window within 46 ms every time.

## What Changes

- The desktop window receives session events through the Wails runtime event bridge, not the custom-scheme HTTP event stream. The frames, their order, and the generation and sequence identity stay the same.
- The bridge stream requires the per-launch credential, as the HTTP event stream does.
- Browser and test-harness clients keep using the HTTP event stream.

## Capabilities

### New Capabilities

### Modified Capabilities

- `gui-local-api`: session events reach the desktop window without waiting for later traffic, and the window event bridge requires the per-launch credential.

## Impact

GUI local API (`internal/localapi`), desktop host wiring (`cmd/agent-marketplace-gui`), GUI client (`gui/src`), and a native Linux CI check. No CLI or TUI behavior changes. Wails and vendored code are unchanged.

## Release note

Sync and Update completion notifications in the desktop app now appear as soon as the operation finishes.
