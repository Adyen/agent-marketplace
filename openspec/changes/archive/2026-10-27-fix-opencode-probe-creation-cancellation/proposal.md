## Why

Cancelling the offline OpenCode probe immediately after container creation
starts can run cleanup before its creator PID is recorded. Cleanup can miss
the eventual container ID and leave an owned container behind.

## What Changes

- Defer cancellation until the creator and watchdog PIDs are recorded.
- Add direct and process-group cancellation regressions at that exact startup
  boundary, retaining the existing collision and running-container controls.
- Include captured launcher output when a cleanup assertion fails.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `opencode-hooks`: record the offline probe's cancellation ownership contract.

## Impact

Only the developer probe launcher and its tests change. No shipped CLI,
consumer configuration, native adapter or CI-policy changes.

## Release note

none - Fixes cancellation cleanup in the developer-only offline OpenCode probe.
