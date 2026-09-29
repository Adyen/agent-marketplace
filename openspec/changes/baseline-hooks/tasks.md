## 1. Bind ingestion scenarios

Before annotating each test, read its assertions. If a test does not prove the
scenario as written, narrow the scenario to what current code does (design
D3). Do not change production code in this change.

- [ ] 1.1 Annotate `TestLoadHooksWrappedAndWorkspaceFlatRemainSupported`
  (`pkg/harness-compilation/bundle/hook_reserved_test.go`) with
  `// spec: hook-resources/workspace-and-wrapped-forms-flatten-identically`;
  annotate `TestLoadHooks_AssignsDistinctNamesToRepeatedEventTypes` and
  `TestLoadHooks_SuffixedNameDoesNotCollideWithTypedBaseName`
  (`bundle/hook_test.go`) with
  `// spec: hook-resources/repeated-event-and-type-receive-distinct-names`;
  verify `go test ./pkg/harness-compilation/bundle -run 'TestLoadHooks' -count=1`
  passes
- [ ] 1.2 Annotate `TestLoadHooksRejectsDuplicateKeys`
  (`bundle/duplicatekey_test.go`) with
  `// spec: hook-resources/duplicate-json-keys-reject-the-hook-file` and
  `TestLoadHooks_HookTypeField` (`bundle/hook_test.go`) with
  `// spec: hook-resources/non-command-handler-type-loads-with-a-warning`.
  If `TestLoadHooks_HookTypeField` does not assert
  `hook.claude.unsupported-type`, add that assertion as a test-only change.
  Verify the bundle tests pass
- [ ] 1.3 Annotate native reversal tests:
  `TestLoadHooksAliasDistinctNames` (`ingest/windsurf/additive_hooks_test.go`)
  → `windsurf-native-file-keeps-every-handler`;
  `TestLoadHooksMapsBeforeAgentToUserPromptSubmit`
  (`ingest/gemini/bundle_test.go`) →
  `gemini-beforeagent-maps-to-userpromptsubmit`; `TestHookIngest`
  (`ingest/devin/devin_test.go`) and
  `TestDevinReverseHookEventAgreesWithForwardTable`
  (`compile/mapping/devin_test.go`) →
  `undocumented-devin-event-keeps-its-name-with-a-diagnostic`;
  `TestUnsupportedHooksAreSuppressedDuringIngestion`
  (`ingest/codex/codex_test.go`) → `unsupported-codex-handlers-are-skipped`;
  `TestLoadHooks_NormalizesCursorPluginRootToken`
  (`ingest/cursor/hook_test.go`) → `cursor-plugin-root-token-is-normalized`.
  Verify `go test ./pkg/harness-compilation/ingest/... ./pkg/harness-compilation/compile/mapping -count=1`
  passes

## 2. Bind compilation scenarios

- [ ] 2.1 Annotate `TestAdditiveHooksAllTargetsScopesAndModes`
  (`internal/materialize/additive_hooks_safety_test.go`) with
  `every-harness-target-receives-hooks-in-both-scopes-and-modes`, and confirm
  it asserts every path in the output table. Annotate `TestClaudeHook_Passthrough`
  (`compile/claude/translator_test.go`) and `TestMatrix_Hook`
  (`pkg/harness-compilation/matrix_test.go`) with
  `claude-code-hooks-use-the-plugin-hook-file`, and `TestClaudeHook_Passthrough`
  with `claude-code-emits-unknown-events-unchanged`. Verify the tests pass
- [ ] 2.2 Annotate event-loss tests: `TestCompileHookEventMapping`
  (`compile/gemini/translator_test.go`) and `TestTranslateHook_UnmappableEvent`
  (`compile/windsurf/translator_test.go`) →
  `gemini-and-windsurf-suppress-unmappable-events-with-a-warning`;
  `TestFactoryHookEventsMustBeRepresentable`
  (`restriction_boundary_test.go`), `TestHookUnsupportedCombinationsSkipTheEntry`
  (`compile/devin/hook_test.go`), and
  `TestUnsupportedRuleAndHookProduceStableDiagnostics`
  (`compile/codex/translator_test.go`) →
  `factory-devin-and-codex-suppress-unmappable-events-with-an-error`;
  `TestTranslateHook_PreToolUse_Bash` and `TestTranslateHook_PreToolUse_EmptyMatcher`
  → `windsurf-splits-pretooluse-by-tool-matcher`. Verify the target packages
  pass
- [ ] 2.3 Annotate matcher and timeout tests:
  `TestNativeHookDefinitionsUseMatcherGroups` (`compile/gemini/hook_native_test.go`)
  → `gemini-receives-the-matcher-and-timeout-verbatim`;
  `TestTranslateHookTimeoutSuppressesArtifact` → `windsurf-refuses-explicit-timeouts`;
  `TestNativeSessionEndCapsTimeout` (`compile/codex/translator_test.go`) →
  `codex-caps-sessionend-timeouts`; `TestHookSuppressesNegativeTimeout` (Codex)
  and the timeout-invalid case of `TestHookUnsupportedCombinationsSkipTheEntry`
  (Devin) → `negative-timeouts-are-refused-for-devin-and-codex`. Verify the
  tests pass
- [ ] 2.4 Annotate `TestD2PromptHookSupportAndSuppression`
  (`d2_repair_test.go`) with `prompt-hooks-compile-where-supported` and
  `prompt-hooks-are-suppressed-where-unsupported`, and
  `TestFactoryHook_NonCommandTypeDropped` with
  `factory-drops-other-non-command-types-with-a-warning`. Verify the tests
  pass
- [ ] 2.5 Annotate plugin-root tests: `TestTranslate_Hook`
  (`compile/cursor/translator_test.go`) and `TestCompileHookToSettingsJSON`
  (`compile/gemini/translator_test.go`) →
  `cursor-and-gemini-receive-harness-variables`;
  `TestRunExpandsCodexPluginRootFromBundle` (`internal/materialize`),
  `TestHookResolvesPluginRootFromOptions` (Devin), and
  `TestTranslateHook_WithPluginRoot` (Windsurf) →
  `codex-devin-and-windsurf-receive-the-source-bundle-root`;
  `TestHookPluginRootResolution` (Codex), `TestTranslateHook_WithoutPluginRoot`,
  and the unresolved case of `TestHookResolvesPluginRootFromOptions` →
  `missing-plugin-root-leaves-the-token-with-a-warning`. Verify the tests pass
- [ ] 2.6 No existing test asserts the materialized Factory command after
  `${DROID_PLUGIN_ROOT}` expansion. Add a test-only characterization in
  `internal/materialize/materialize_test.go` that syncs a repository-scope
  Factory hook whose command is `${CLAUDE_PLUGIN_ROOT}/hooks/check.sh` and
  asserts the written command is `.factory/hooks/check.sh`. Annotate it with
  `factory-commands-point-into-the-install-root`. Verify
  `go test ./internal/materialize -run <new test> -count=1` passes on the
  unchanged production code

## 3. Bind materialization, ownership, and selection scenarios

- [ ] 3.1 Annotate `TestRunHookSupportFilesAreSymlinked` with
  `support-scripts-are-installed-beside-the-hook-file`, and
  `TestAdditiveHookSupportConflictPreservesInstalledBytes` with
  `conflicting-support-files-refuse-the-affected-hooks`. Verify
  `go test ./internal/materialize -run 'TestRunHookSupport|TestAdditiveHookSupport' -count=1`
  passes
- [ ] 3.2 Annotate `TestRunExcludedHookEntryRemovedFromMergedFile` →
  `deselected-hook-is-removed-from-the-merged-file`;
  `TestRunHookArtifactsAreNotPrunedOnSecondSync` and
  `TestMaterialize_NoOpHookSyncIsByteIdentical`
  (`pkg/harness-compilation/materialize_hook_identity_test.go`) →
  `repeat-sync-is-byte-identical`;
  `TestDevinHandwrittenHooksKeepOrderAndSatisfyIdenticalDesiredEntries` →
  `handwritten-devin-hooks-keep-order-and-satisfy-identical-entries`. Verify
  the tests pass
- [ ] 3.3 Annotate `TestCodexTrustReviewDiagnosticsFollowRepositoryMutation`
  with `codex-repository-hook-change-reports-trust-review`. Verify it passes
- [ ] 3.4 Annotate `TestAdditiveHooksIngestResolveMaterialize` →
  `same-named-hooks-from-two-registries-both-install`;
  `TestInstallCommandAllIncludesCrossRegistryHooks`
  (`cmd/agent-marketplace/install_additive_hooks_test.go`) →
  `install-all-includes-every-hook-provider`;
  `TestResolveAdditiveHooksThroughCollections` →
  `collections-keep-additive-hook-providers`. Verify
  `go test ./internal/resolver ./internal/materialize -run 'Additive' -count=1`
  and
  `go test ./cmd/agent-marketplace -run '^TestInstallCommandAllIncludesCrossRegistryHooks$' -count=1`
  pass
- [ ] 3.5 Annotate `TestDevinBundleBadHookDoesNotSuppressGoodOnes` →
  `a-bad-devin-hook-does-not-suppress-good-ones`;
  `TestWindsurfIncompleteHooksRetainManagedSiblings` →
  `blocked-windsurf-hooks-keep-their-installed-entries`;
  `TestHookDiagnosticsNeverQuotePayloads` →
  `devin-diagnostics-never-quote-payloads`; `TestHookAnalyzer`
  (`pkg/marketplace/analyze/kind_test.go`) →
  `hook-resource-yields-the-capability-finding`;
  `TestWindsurfLegacyHookLockRetainedUntilUpgrade` →
  `legacy-windsurf-hook-lock-is-retained-until-upgrade`. Verify the tests pass

## 4. Harvest legacy documents

- [ ] 4.1 Delete `docs/plans/2026-09-16-additive-hooks.md` and check off row
  4.17 in `openspec/changes/harvest-legacy-docs/tasks.md`. Verify the file is
  gone and every carried claim has a `// spec: hook-resources/...` test
- [ ] 4.2 Delete the `AGENTS.md` appendix section "Additive hook selection" and
  check off row 6.8. Verify the section is gone and nothing links to it
- [ ] 4.3 Repoint remaining links to the deleted plan (code comments, skills,
  `site/`) at `openspec/specs/hook-resources/spec.md`. Verify searching for
  `2026-09-16-additive-hooks` finds nothing outside `openspec/changes/archive`

## 5. Validate and archive

- [ ] 5.1 Run `openspec validate baseline-hooks --strict` and
  `go run ./tools/specaudit trace`. Verify there are no unbound
  `hook-resources` scenarios and no dangling annotations
- [ ] 5.2 Run `make validate` and `make spec-check`. Verify both pass
- [ ] 5.3 Run `openspec archive baseline-hooks --yes` in this pull request.
  Verify `openspec validate --all --strict` and `make spec-check` pass
  afterward
