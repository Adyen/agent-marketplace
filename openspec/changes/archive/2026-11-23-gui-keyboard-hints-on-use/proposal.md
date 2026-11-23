## Why

The status bar shows keyboard hints ("move", "open", "sync") to every user, including people who only use the pointer. Those hints are noise until a keyboard user needs them.

## What Changes

- The status bar's keyboard hints are hidden at startup.
- The hints appear after the first arrow, Enter, or Tab key press in the session, and stay visible for the rest of the session.
- Pointer use alone does not show the hints.

## Capabilities

### Modified Capabilities

- `gui-shell`: the status bar keyboard hints appear only after keyboard navigation.

## Impact

Desktop GUI status bar (`gui/src/StatusBar.vue`) and its tests. No keyboard shortcut, command, or action changes.

## Release note

The desktop app's status bar shows its keyboard hints after you first navigate with the keyboard.
