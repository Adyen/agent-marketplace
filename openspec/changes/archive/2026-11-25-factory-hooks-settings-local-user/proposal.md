## Why

Factory Droid loads hooks from three files within a single `.factory/` directory: `settings.local.json` (under `hooks`), `hooks.json` (top-level event map), and `settings.json` (under `hooks`). Per Factory's Hooks Reference, any event defined in `hooks.json` replaces the same event in `settings.json` and causes Droid to report a shadowed-hook diagnostic, whereas `settings.local.json#hooks` concatenates with both `settings.json#hooks` and `hooks.json` without shadowing either and is not shadowed when a user later saves hooks via Droid's `/hooks` UI.

Writing machine-scope Factory Droid hooks to `~/.factory/hooks.json` silently disables any existing `~/.factory/settings.json` hooks for the same event. Switching machine-scope hooks to `~/.factory/settings.local.json` under `hooks` resolves this coexistence break immediately, while adding read-only repository-scope satisfaction tolerance for `.factory/settings.local.json#hooks` prepares released clients before a follow-up change switches repository-scope writes.

## What Changes

- Machine-scope Factory Droid hooks materialize into `~/.factory/settings.local.json` under the `hooks` member instead of `~/.factory/hooks.json`.
- Owned hook entries previously written to `~/.factory/hooks.json` migrate atomically into `~/.factory/settings.local.json#hooks`: only ownership-proven entries are pruned from `~/.factory/hooks.json`, empty event arrays are removed so `hooks.json` does not clear `settings.json` events, `~/.factory/hooks.json` is deleted when empty, and handwritten entries remain untouched. Malformed or unsafe retired files and unavailable ownership state fail closed without modifying either file.
- Marked legacy entries in `~/.factory/hooks/hooks.json` continue to migrate cleanly into `~/.factory/settings.local.json#hooks` without confusing their top-level event layout with the wrapped `hooks` member of `settings.local.json`.
- Deselecting all machine-scope Factory Droid hooks after upgrading still prunes owned entries from retired `~/.factory/hooks.json`.
- Repository-scope Factory Droid hooks continue writing to `.factory/hooks.json` for compatibility with currently released clients, while read-only forward satisfaction recognizes complete, verified selected repository hooks already present in `.factory/settings.local.json#hooks` without writing `.factory/hooks.json`, modifying `.factory/settings.local.json`, or creating ownership state.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `hook-resources`: Factory Droid machine-scope hooks materialize into `~/.factory/settings.local.json#hooks`, migrate owned entries out of retired `~/.factory/hooks.json`, and recognize forward repository-scope hooks already satisfied in `.factory/settings.local.json#hooks`.

## Impact

Factory Droid native layout metadata (`pkg/harness-compilation/target`), native hook reconciliation, retired-file migration, inspection, and removal preflight (`internal/materialize`), backup coverage verification (`internal/harness`, `internal/pipeline`), CLI/E2E hook tests (`cmd/agent-marketplace`, `e2e`), and harness output-path documentation (`site/docs/reference/harnesses-and-output-paths.md`, `skills/agent-marketplace/SKILL.md`, `docs/harness-validation/factory.md`). No consumer configuration, acquisition lock, Marketplace API, or TUI behavior changes.

## Release note

Factory Droid user-scope hooks now sync to `~/.factory/settings.local.json` under `hooks` instead of `~/.factory/hooks.json`, preventing managed hooks from shadowing hooks declared in `~/.factory/settings.json`. Existing owned entries in `~/.factory/hooks.json` migrate automatically on `agent-marketplace sync`, leaving handwritten entries untouched and removing `~/.factory/hooks.json` when empty.
