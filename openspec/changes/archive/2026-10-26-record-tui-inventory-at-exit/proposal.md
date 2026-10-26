## Why

The TUI records effective-configuration inventory after every accepted final snapshot, so each toggle, edit, reload, sync, or update emits a new full set of fingerprinted resource series. About half of 30-day resource series come from configurations superseded within ten minutes, which inflates Prometheus series churn without adding information about the configuration a user actually left in effect.

## What Changes

- An interactive TUI session records effective-configuration inventory once, when the session ends, for the latest accepted complete snapshot.
- Nothing is recorded during the session. Operations the session starts (snapshot reloads, sync, update, and configuration mutations) do not record their own inventory.
- When the latest accepted snapshot has hard resolution failures, the session records no inventory.
- CLI commands are unchanged; they keep recording inventory at their own boundaries.

## Capabilities

### Modified Capabilities
- `telemetry`: adds an interactive-session inventory requirement that defines when TUI inventory is recorded.

### New Capabilities
- None.

## Impact

- `internal/pipeline`: a context marker suppresses inventory recording for Resolve and explicit recording calls.
- `internal/tui`: the session holds its latest accepted final snapshot and records it on exit; the session context carries the suppression marker.
- Telemetry: fewer `config.snapshot`, `config.registry`, and `config.resource` series for interactive sessions. A session that is killed without its exit path may record nothing.
