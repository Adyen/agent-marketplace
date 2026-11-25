## Context

Factory Droid evaluates hooks within one `.factory/` directory in precedence order: `settings.local.json` (`hooks` member, concatenated), `hooks.json` (top-level event map, replaces same event in `settings.json`), and `settings.json` (`hooks` member). Because `hooks.json` replaces matching events in `settings.json` and triggers a Droid diagnostic warning, Agent Marketplace's current `~/.factory/hooks.json` output breaks coexistence with hooks in `~/.factory/settings.json`.

Repository-scope shared checkouts are verified against the pinned stable release (`v4.1.4`) in `released-client-gate`, which only recognizes `.factory/hooks.json` at repository scope. Switching repository-scope writes therefore requires a reader-first release before switching repository writes in a follow-up change.

## Goals / Non-Goals

**Goals:**
- Switch Factory Droid machine-scope hooks to `~/.factory/settings.local.json` under `hooks` immediately, with zero target-specific branches added to `internal/materialize` (preserving `TestHookArchitectureProductionTree`).
- Atomically migrate ownership-proven entries out of retired `~/.factory/hooks.json` using `ownership.Store` (`PrepareBatch` + `dependencytxn.Commit`), removing empty event arrays and deleting `hooks.json` when empty while preserving handwritten entries and failing closed on malformed or corrupt state.
- Preserve v4 legacy marked-file migration from `~/.factory/hooks/hooks.json` (whose top-level event map has an empty member even though `settings.local.json` uses member `hooks`).
- Add read-only repository-scope forward satisfaction for `.factory/settings.local.json#hooks` so a release carrying this change verifies repositories whose hooks have been moved to `.factory/settings.local.json#hooks` by a later client.

**Non-Goals:**
- Switching repository-scope Factory Droid writes away from `.factory/hooks.json` (deferred to the stacked follow-up PR once this change ships in a stable release).
- Changing consumer configuration, acquisition lock formats, or TUI behavior.

## Decisions

1. **Data-driven retired native file and legacy container metadata on `NativeAggregateFile`:**
   - Extend `target.NativeAggregateFile` with:
     - `LegacyMember` (or per-legacy-file container shape) so legacy marked files whose envelope differs from the active native file's `Member` (such as Factory `hooks/hooks.json` top-level events vs `settings.local.json#hooks`) are parsed with their true container shape.
     - `RetiredNative []NativeRetiredAggregateFile` declaring previously owned native hook files (`Path`, `Member`, `Envelope`) that must be pruned using ownership proof rather than `_managed_by` markers.
     - `ForwardNative []NativeRetiredAggregateFile` (or `ForwardHookLocations`) declaring future native hook file shapes checked read-only for complete satisfaction before emitting into the current repository hook file.
   - Validate all three in `ValidateNativeLayouts` (clean relative paths, distinct from `Path`, valid member/envelope, no collisions).

2. **Atomic retired-file ownership migration via `Store.PrepareBatch` + `dependencytxn.Commit`:**
   - Inside `nativeHookActivationFinalizer`, inspect each declared `RetiredNative` file via `ownership.ReadAggregateFileAt` and `store.Proof(retiredRel)`.
   - If the retired file exists: parse strictly with `jsonc.Parse`, classify entries against `store.Proof`, prune only `StatusOwnedClean` entries, delete empty event arrays, delete the file when `len(root.Keys()) == 0`, take a safety backup, and fold the retired file's cleared `ownership.Mutation` into the same `Store.PrepareBatch` state write and `dependencytxn.Commit` as the active native file (`settings.local.json`).
   - If the retired file is malformed, has duplicate keys, has an invalid envelope, or its ownership state is corrupt, fail closed for this `(target, scope)` without modifying either file.
   - If all hooks are deselected immediately after upgrade, `nativeHookOutputPresent` detects owned hook entries in any declared `RetiredNative` file so reconciliation still runs and cleans up `~/.factory/hooks.json`.

3. **Read-only repository-scope forward satisfaction:**
   - Before compiling/writing a repository-scope hook into the current native file (`.factory/hooks.json`), check any declared `ForwardNative` location (`.factory/settings.local.json#hooks`) for complete satisfaction of that selected hook:
     - Durable ownership proof or satisfaction proof for the exact `(resource, sourceDigest)`, OR
     - Complete source-derived independent satisfaction (every expected canonical entry present in `.factory/settings.local.json#hooks` + verified regular portable support files under `.factory/hook-support/<source>/<hook>/`).
   - Never mutate `.factory/settings.local.json` or create ownership state when checking forward satisfaction; fail closed with `hook.native-tolerance-refused` (or `json.duplicate-key`) on malformed or unsafe `.factory/settings.local.json`.

## Risks / Trade-offs

- **Empty event arrays in retired `hooks.json`:** Factory treats `"PreToolUse": []` in `hooks.json` as clearing `settings.json#hooks.PreToolUse`. Deleting empty event keys and deleting the file when no keys remain eliminates any residual shadowing.
- **Coexistence with `settings.local.json` handwritten content:** `owned_json_reconcile.go` uses format-preserving JSONC edits and leaves unrelated settings and handwritten hooks intact; `nativeHookJSONLayout` preserves `enabledPlugins`/`extraKnownMarketplaces` when sharing a settings file.
