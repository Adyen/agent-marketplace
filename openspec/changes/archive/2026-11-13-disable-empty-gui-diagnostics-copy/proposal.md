# Proposal

## Why

The Diagnostics view allows Copy diagnostics to be activated when there are no messages, which can write empty clipboard contents and show misleading success feedback.

## What Changes

- Disable Copy diagnostics when the diagnostics list is empty.
- Specify that the empty-state control does not write to the clipboard or show successful-copy feedback.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `gui-shell`: Specify empty diagnostics copy behavior.

## Impact

- Desktop GUI diagnostics view and its workspace tests.

## Release note

Disable Copy diagnostics when the Diagnostics view has no messages.
