## 1. Layout metadata and target declarations

- [x] 1.1 Extend `target.NativeAggregateFile` with validated data-driven retired native file, legacy member, and forward native reader declarations; switch `TargetFactory` user-scope hooks to `settings.local.json#hooks` (retiring `hooks.json`, keeping `hooks/hooks.json` top-level legacy) and declare `settings.local.json#hooks` as a forward reader location on `TargetFactory` project scope.
- [x] 1.2 Update `pkg/harness-compilation` target, native layout, and hook projection unit tests for Factory Droid user-scope `settings.local.json#hooks`.

## 2. Retired native migration, legacy member handling, and forward reader

- [x] 2.1 Implement atomic ownership-backed retired native hook file migration in `internal/materialize/native_hook_activation.go` and `owned_json_reconcile.go` (using `Store.PrepareBatch` + `dependencytxn.Commit`), including empty event-key removal, empty-file deletion, handwritten preservation, safety backup, and fail-closed refusal on malformed/corrupt input.
- [x] 2.2 Update `nativeHookLegacyPatch` to use each legacy file's declared container member so v4 Factory `hooks/hooks.json` (top-level event map) continues migrating cleanly to `settings.local.json#hooks`.
- [x] 2.3 Update `nativeHookOutputPresent`, `nativeHookPortableProtectedPaths`, `nativeHookExcludedSupportFiles`, `nativeHookLayoutFor`, and `HasActiveNativeHooks` for retired native files and Factory user-scope `settings.local.json#hooks`.
- [x] 2.4 Implement read-only repository-scope forward satisfaction in `internal/materialize` for declared forward native hook locations (`.factory/settings.local.json#hooks`), recognizing complete durable or independently reconstructed satisfaction without writing `.factory/hooks.json` or mutating `.factory/settings.local.json`.

## 3. Tests, documentation, and verification

- [x] 3.1 Add and annotate tests (`// spec: hook-resources/...`) covering every new scenario: machine-scope `settings.local.json#hooks` output, atomic owned migration and deletion of empty `hooks.json`, preservation of handwritten `hooks.json` entries without empty event arrays, fail-closed refusal on malformed/corrupt retired `hooks.json`, zero-hook deselection cleanup of retired `hooks.json`, v4 `hooks/hooks.json` migration, and repository-scope forward satisfaction for `.factory/settings.local.json#hooks`.
- [x] 3.2 Update existing Factory machine-scope test expectations and docs (`site/docs/reference/harnesses-and-output-paths.md`, `skills/agent-marketplace/SKILL.md`, `docs/harness-validation/factory.md`).
- [x] 3.3 Run `make spec-check`, `make validate`, `make validate-e2e`, `go -C e2e test -run '^TestTableSpecTags' -count=1 .`, and `go run ./tools/specaudit gate -base origin/main -head HEAD`.
- [x] 3.4 Run `openspec archive factory-hooks-settings-local-user --yes` as the final commit of the pull request.
