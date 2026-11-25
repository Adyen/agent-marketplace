## Why

Factory Droid's `.factory/hooks.json` replaces matching hook events in `.factory/settings.json` in the same repository directory and triggers a shadowed-hook diagnostic, whereas `.factory/settings.local.json#hooks` concatenates with both `.factory/settings.json#hooks` and `.factory/hooks.json`.

A prior release switched machine-scope Factory Droid hooks to `~/.factory/settings.local.json#hooks` and added read-only repository-scope forward satisfaction for `.factory/settings.local.json#hooks`. This follow-up switches repository-scope Factory Droid hooks to `.factory/settings.local.json#hooks` so both scopes avoid shadowing `settings.json`.

## What Changes

- Repository-scope Factory Droid hooks materialize into `.factory/settings.local.json` under the `hooks` member instead of `.factory/hooks.json`, coexisting with `.factory/settings.local.json` plugin registrations (`enabledPlugins`, `extraKnownMarketplaces`) and handwritten settings.
- Owned hook entries previously written to `.factory/hooks.json` migrate atomically into `.factory/settings.local.json#hooks`, preserving handwritten entries, removing empty event keys, and deleting `.factory/hooks.json` when empty.
- Marked legacy entries in `.factory/hooks/hooks.json` continue migrating out of its top-level event map into `.factory/settings.local.json#hooks`.
- Prerequisites before merging: the prior reader release must be published as a stable release and pinned with verified checksums in `released-client-gate`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `hook-resources`: Factory Droid repository-scope hooks materialize into `.factory/settings.local.json#hooks`, coexist with `.factory/settings.local.json` plugin registrations, and migrate owned entries out of retired `.factory/hooks.json`.

## Impact

Factory Droid repository-scope native layout (`pkg/harness-compilation/target/native_layout.go`), native hook inspection (`internal/materialize/native_hook_inspect.go`), unit/command/E2E hook tests (`pkg/harness-compilation`, `internal/materialize`, `cmd/agent-marketplace`, `e2e`), and harness output-path docs. No consumer configuration, acquisition lock, Marketplace API, or TUI behavior changes.

## Release note

Factory Droid repository-scope hooks now sync to `.factory/settings.local.json` under `hooks` instead of `.factory/hooks.json`, preventing managed hooks from shadowing hooks declared in `.factory/settings.json`. Existing owned entries in `.factory/hooks.json` migrate automatically on `agent-marketplace sync`, leaving handwritten entries intact and deleting `.factory/hooks.json` when empty.
