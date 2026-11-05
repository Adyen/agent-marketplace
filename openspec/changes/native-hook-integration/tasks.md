## 0. Preconditions

- [x] 0.1 Confirm `baseline-hooks` is archived and
  `openspec/specs/hook-resources/spec.md` exists. Verify that
  `openspec validate native-hook-integration --strict` passes, and that a dry
  archive in a scratch copy of `openspec/` applies with no dropped-scenario
  error
- [ ] 0.2 Confirm that a release containing `hook-native-location-tolerance`
  (design, Migration Plan step 0) is the latest release pinned in
  `e2e/released_client_compat_test.go`. Verify by reading the pinned version
  and its changelog entry. Do not merge section 6 until this holds
- [x] 0.3 Rebase on PR #276 if it has merged, so that `make catalog-check` and
  the drift audit can extend its targets and tool. If it has not merged,
  keep `modelpolicy` untouched and add the audit sources in a follow-up.
  Verify `make model-catalog-check` still passes either way

## 1. Catalogs (pkg/harness-compilation)

- [x] 1.1 Re-verify every entry in the draft catalogs in
  `openspec/changes/native-hook-integration/catalogs/` against its evidence
  refs, and update `evidence.checked` where a page changed. Copy them to
  `toolpolicy/concepts.yaml`, `toolpolicy/catalogs/<harness>.yaml`, and
  `hookpolicy/catalogs/<harness>.yaml`. Verify each file's refs are
  first-party
- [x] 1.2 Add `toolpolicy`: an embedded strict loader, validation (design
  D2), and indexes built once. Write red tests first: unknown key,
  duplicate key, unknown concept, colliding alias, surface narrowing to an
  undeclared surface, missing evidence. Annotate
  `// spec: harness-tool-catalogs/colliding-alias-fails-the-check`. Verify
  `go test ./pkg/harness-compilation/toolpolicy -count=1` passes
- [x] 1.3 Add `hookpolicy`: an embedded strict loader and validation (design
  D2). Write red tests first: overlapping split events, a type on an event
  that allows none, bad unit, bad repository base, invalid or duplicate env
  name, missing evidence. Annotate
  `hook-resources/invalid-catalog-fails-the-check`. Verify
  `go test ./pkg/harness-compilation/hookpolicy -count=1` passes
- [x] 1.4 Copy `catalogs/testdata/conversions.yaml` to
  `hookpolicy/testdata/conversions.yaml`, and add a fixture runner through
  the public `Compile` path. The fixtures must fail on the unchanged
  translators before sections 2–3 land. Verify the red run, then the green
  run after section 3
- [x] 1.5 Add `make catalog-check`. It runs `toolpolicy`/`hookpolicy`
  validation, the conversion fixtures, the matrix goldens, PR #276's
  `model-catalog-check` when present, and the generated-docs freshness
  check. Add `make catalog-docs`, generating
  `site/docs/reference/hook-and-tool-names.md` from the catalogs. Annotate
  `hook-resources/shipped-catalogs-pass-the-check` and
  `harness-tool-catalogs/stale-generated-reference-fails-the-check`. Verify
  both targets run offline
- [x] 1.6 Add `HookSource` to `ir.Hook`, and `ToolSource` to `ir.Subagent` and
  `ir.Skill`. Cover canonical round-trips, extras-collision rejection, and
  case-alias rejection. Verify with `go test ./pkg/harness-compilation/ir ./pkg/harness-compilation/artifact -count=1`
- [x] 1.7 Extend `target.NativeLayout` for all seven targets with the hook
  aggregate (path, member, envelope, shape), the support directory root, and
  legacy locations. Windsurf's repository file is `.devin/hooks.json`, with
  legacy `.windsurf/hooks.json`. Move the Factory, Cursor, Windsurf, and
  Gemini hook declarations out of `internal/harness`. Verify
  `ValidateTargetRegistrations` and the native layout tests pass
- [x] 1.8 Add the new shared diagnostic codes (design D13) and raise existing
  loss codes to error severity where the spec requires it. Verify the diag
  catalog tests pass

## 2. Tool translation on toolpolicy

- [x] 2.1 Implement concept-set translation (design D5): exact coverage for
  hook matchers, subset-only for allow-lists, plus matcher parsing, output
  syntax, anchoring, and MCP pattern rewriting. Keep `WithToolOverride` as
  the per-invocation override. Verify `go test ./pkg/harness-compilation/toolpolicy -count=1`
  passes against the `matchers` and `allow_lists` fixtures
- [x] 2.2 Move subagent `tools` translation in the Claude, Factory, Cursor,
  Gemini, and Devin compilers, and Devin skill `allowed-tools`, onto
  `toolpolicy`, keeping each target's existing unknown-tool codes and
  effects. Annotate:
  - `factory-droids-receive-factory-tool-ids`
  - `gemini-agents-receive-gemini-tool-names`
  - `write-and-edit-reach-every-factory-file-change-tool`
  - `a-combined-tool-is-not-granted-for-part-of-its-scope`
  - `cursor-subagents-with-a-tool-list-are-suppressed`
  - `devin-keeps-its-narrow-allowed-tools-vocabulary`

  Refresh the subagent matrix goldens deliberately and review the diff.
  Verify `go test ./pkg/harness-compilation/... -count=1` passes
- [x] 2.3 Move Devin and Cursor ingestion tool reversal onto `toolpolicy` and
  record `ToolSource`. Annotate
  `harness-tool-catalogs/renamed-tools-are-accepted-through-aliases` and
  `gemini-wildcards-survive-same-harness-output`. Verify the ingest tests
  pass
- [x] 2.4 Delete `compile/mapping/tools.go`, apart from the frozen legacy alias
  table. Verify no production code references `mapping.Tool`,
  `ToolWithOverride`, or `ReverseTool`

## 3. Hook translators on hookpolicy, one target at a time

For each target, write red tests first, switch the translator to
`hookpolicy.Resolve`, delete that target's hard-coded checks, and refresh its
hook matrix goldens deliberately.

- [x] 3.1 Implement `hookpolicy.Resolve` (design D4 and D6): events, types,
  timeouts, matcher via `toolpolicy`, and env and plugin-root rewriting
  against an opaque support base. Verify the `events`, `env`, and `timeouts`
  fixtures pass
- [x] 3.2 Gemini CLI. Annotate
  `gemini-timeouts-are-converted-to-milliseconds`,
  `gemini-millisecond-timeouts-round-up-on-ingestion`,
  `gemini-hooks-keep-millisecond-timeouts`, and
  `mcp-matchers-follow-the-target-s-mcp-naming`
- [x] 3.3 Windsurf. Annotate
  `windsurf-splits-pretooluse-by-translated-tool-matcher` and
  `windsurf-refuses-explicit-timeouts`
- [x] 3.4 Devin CLI and Codex. Annotate `codex-caps-sessionend-timeouts`,
  `negative-timeouts-are-refused-on-every-target`, and
  `tool-names-are-translated-per-target`
- [x] 3.5 Claude Code. Annotate `claude-code-refuses-unknown-events`,
  `unknown-handler-types-are-refused-on-claude-code`,
  `prompt-hooks-compile-where-the-catalog-declares-them`, and
  `renamed-tools-are-accepted-through-aliases`
- [x] 3.6 Cursor. Annotate `cursor-receives-native-event-names`
- [x] 3.7 Factory Droid. Annotate `factory-hooks-keep-native-tool-names`
- [x] 3.8 Add cross-target tests through the public `Compile` path and
  annotate them:
  - `unmappable-events-are-errors-on-every-target`
  - `differently-timed-events-are-never-equated`
  - `write-and-edit-translate-to-each-target-s-file-change-tools`
  - `partial-tool-coverage-suppresses-the-hook`
  - `untranslatable-matchers-suppress-the-hook`
  - `prompt-hooks-are-refused-where-undeclared`
  - `project-directory-references-follow-the-target`
  - `targets-that-accept-the-claude-spelling-keep-it`
  - `variables-without-a-target-equivalent-refuse-the-hook`
  - `unknown-variables-are-left-unchanged`

  Verify `go test ./pkg/harness-compilation/... -count=1` passes
- [x] 3.9 Delete `compile/mapping/hook_events.go` and the hook entries in
  `envvars.go`, keeping MCP env mapping. Verify nothing references
  `mapping.HookEvent`, `ReverseHookEvent`, or `NativeCodexHookEvent`

## 4. Ingestion and source revisions

- [x] 4.1 Record `HookSource` in every ingester and stop normalizing native
  values early. Add native-location ingestion for `.claude/settings.json`,
  `.factory/hooks.json`, `.cursor/hooks.json`, and Windsurf's
  `.devin/hooks.json`, with the autodetection rule from the design Risks.
  Annotate:
  - `claude-settings-hooks-are-ingested`
  - `cursor-native-hooks-retain-native-events-on-ingestion`
  - `devin-desktop-workspace-hooks-are-ingested-for-windsurf`

  Add a two-way autodetection test for `.devin/`. Verify the ingest tests
  pass
- [x] 4.2 Preserve the existing source-revision schemas and generator versions
  for hook and tool provenance. Keep historical digest projections unchanged,
  with frozen vectors for every supported version and hook- and tool-bearing
  fixtures for each layout. Do not introduce a new generator version. Annotate
  a CLI lock test with `pre-change-hook-lock-verifies-unchanged`. Verify
  `go test ./internal/sourcerevision -count=1` and the command lock tests pass

## 5. Materialization (internal consumes library declarations only)

- [x] 5.1 Replace the hook support routing with per-hook
  `hook-support/<source>/<hook>/` directories supplied as an opaque base.
  Delete the Factory `${DROID_PLUGIN_ROOT}` byte replacement and the
  cache-root `WithPluginRoot` injection for hooks. Annotate:
  - `repository-scope-commands-use-the-project-directory-variable`
  - `codex-and-windsurf-repository-commands-use-their-documented-base`
  - `machine-scope-commands-use-absolute-support-paths`
  - `no-plugin-only-variable-or-cache-path-is-emitted`
  - `missing-repository-base-refuses-token-commands`
  - `same-named-scripts-from-two-registries-do-not-conflict`
  - `support-files-reach-devin-codex-and-gemini`
  - `deselected-hooks-remove-their-support-directory`

  Verify `go test ./internal/materialize -count=1` passes
- [x] 5.2 Generalize the Devin and Codex ownership-state hook reconciler to
  every target, driven by `NativeLayout` shape and member. Stop emitting
  `_managed_by` for hooks. Annotate:
  - `handwritten-claude-hooks-satisfy-identical-entries`
  - `new-entries-carry-no-in-file-marker`
  - `every-harness-target-receives-hooks-in-its-native-file` (extend
    `TestAdditiveHooksAllTargetsScopesAndModes`)
  - `claude-code-hooks-merge-into-settings`
  - `cursor-hooks-use-the-versioned-native-envelope`
  - `factory-hooks-use-the-standalone-native-file`
  - `windsurf-repository-hooks-use-the-devin-desktop-workspace-file`

  Keep the baseline annotations `deselected-hook-is-removed-from-the-merged-file`,
  `repeat-sync-is-byte-identical`, and
  `handwritten-devin-hooks-keep-order-and-satisfy-identical-entries` passing
  across all targets. Add a Factory reformatted-owned-entry
  drift test, and a test that Devin CLI and Windsurf cleanup never touch each
  other's `.devin/` files. Verify
  `go test ./internal/materialize ./internal/ownership -count=1` passes
- [x] 5.3 Emit `hook.duplicate-execution-risk` from catalog `loads_hooks_of`
  data. Annotate `claude-code-and-cursor-selected-together`,
  `claude-code-and-devin-cli-selected-together`, and
  `a-single-target-produces-no-duplicate-warning`. Verify the tests pass
- [x] 5.4 Add the architecture guard test (design D14). Verify it fails when a
  hook code path in `internal/materialize` compares against a harness
  constant or embeds a plugin-root or project-directory literal, and passes
  on the finished tree

## 6. Legacy migration (merge only after 0.2 holds)

- [x] 6.1 Implement the one-transaction move of `_managed_by` entries from the
  legacy files to native files: Claude Code, Cursor, and Factory Droid
  `hooks/hooks.json` in both scopes, and Windsurf's repository-scope
  `.windsurf/hooks.json`. Delete legacy files that become empty, prune owned
  legacy support files, and refuse malformed legacy files with
  `materialize.hook.legacy-malformed`. Annotate:
  - `owned-factory-entries-move-to-the-native-file`
  - `a-legacy-file-with-only-owned-entries-is-deleted`
  - `owned-windsurf-entries-move-to-the-devin-desktop-workspace-file`
  - `malformed-legacy-files-block-migration-without-changes`
  - `check-reports-a-pending-migration`

  Verify the tests pass
- [x] 6.2 Add an interrupted-migration test using
  `AGENT_MARKETPLACE_E2E_CRASH_AT`. Verify a half-published migration is
  refused by read-only commands and completed by the next mutating command
- [ ] 6.3 Extend `released-client-gate` with a hook migration step using
  shared v4 consumer configuration. Annotate
  `latest-release-still-verifies-after-migration`. Verify the pinned stable
  v4 client still passes, with every required scenario witnessed by
  `tools/witness-check -require`. Keep independent legacy native-hook
  upgrade coverage under v4 consumer fixtures; do not restore the retired
  pre-v4 consumer success `upgrade-gate` or require a pre-v4 executable.

## 7. Review report and value-free diagnostics

- [x] 7.1 Add hook entry changes to the materialization report, and render a
  "Hook changes" section for `sync` and `sync --check`. Emit
  `materialize.hook.review` and `materialize.hook.review-pending`, keeping the
  Codex codes. Annotate:
  - `changed-hook-commands-are-listed-with-exact-values`
  - `check-reports-pending-hook-changes-without-writing`
  - `unchanged-hooks-produce-no-review`
  - `codex-keeps-its-trust-review-codes`

  Verify the command tests pass through `isolateCommandTest`
- [x] 7.2 Add a cross-target test proving that no hook diagnostic or log
  record contains payload values, and that a sync printing a review writes no
  command to the log file or telemetry. Annotate
  `diagnostics-never-quote-payloads-on-any-target` and
  `review-commands-never-reach-logs`. Verify the tests pass

## 8. Maintainer documentation and drift audit

- [x] 8.1 Add an "Update a shipped hook or tool catalog" recipe to
  `pkg/harness-compilation/README.md`, next to the model recipe, following
  design D3 step by step. Verify that a dry run of the recipe works: rename
  a tool in a scratch branch, and confirm that only the YAML, one fixture
  line, and the regenerated doc change, and that `make catalog-check`
  passes
- [x] 8.2 Add the hook and tool documentation sources from the catalog
  `evidence.refs` to PR #276's catalog audit tool, with sha256 baselines.
  Verify `go test` for the audit tool passes offline
- [x] 8.3 Update the hook rows in
  `site/docs/reference/harnesses-and-output-paths.md`, the hook and tool
  sections of `docs/harness-validation/*.md` (including the third-party
  loaders and the Windsurf `.devin/hooks.json` path), the hook and tool rows
  of `pkg/harness-compilation/diag/codes.md`, and the `registry-authoring`
  skill's hook guidance. Verify the site build and `make spec-check` pass
- [x] 8.4 Update the harness compatibility fixtures to seed hooks in native
  locations. Verify `make validate-e2e` passes

## 9. Validate and archive

- [x] 9.1 Run `go run ./tools/specaudit trace`. Verify there are no unbound
  `hook-resources` or `harness-tool-catalogs` scenarios and no dangling
  annotations
- [x] 9.2 Run `make validate`, `make spec-check`, `make validate-e2e`,
  `make catalog-check`, and `make tui-qa` if TUI hook views changed. Verify
  all pass
- [ ] 9.3 Run `openspec archive native-hook-integration --yes` in this pull
  request. Verify `openspec validate --all --strict` and `make spec-check`
  pass afterward

## 10. Plugin-root tokens mean the bundle root (#733)

- [x] 10.1 Write red tests first for the new `hook-resources` scenarios:
  `bundle-root-references-resolve-on-every-source-path`,
  `git-and-mirror-delivery-produce-the-same-support-layout`,
  `a-reference-to-a-missing-support-file-refuses-the-hook`,
  `bare-and-dynamic-plugin-root-references-are-not-checked`,
  `legacy-folder-relative-references-keep-working-with-a-warning`,
  `references-that-resolve-under-both-meanings-use-the-bundle-root`,
  `support-files-keep-their-bundle-relative-paths`, and
  `folder-relative-support-from-an-earlier-sync-moves-in-one-sync`. Verify
  each fails on the base for the intended reason
- [x] 10.2 Install non-mirror hook support at bundle-relative paths, keeping
  mirror publication, acquisition-lock bytes, and support inventory hashing
  unchanged. Verify the focused `internal/materialize` tests pass
- [x] 10.3 Check literal plugin-root references against the installed
  support set: emit `hook.support-reference-missing` (error, hook suppressed)
  and the legacy fallback with `hook.support-reference-legacy` (warning).
  Register both codes in `pkg/harness-compilation/diag`. Verify the focused
  `hookpolicy` and `internal/materialize` tests pass
- [x] 10.4 Prove an interrupted support move is refused by read-only commands
  and completed by the next mutating command, with no staging residue
- [x] 10.5 Update existing fixtures that use the folder-relative form, the
  author guide `site/docs/how-to/author/publish-a-hook-with-support-files.md`,
  and `pkg/harness-compilation/diag/codes.md`. Verify `make spec-check`,
  `make validate`, and `make validate-e2e` pass
- [x] 10.6 Check the quote-only idiom `"${CLAUDE_PLUGIN_ROOT}"/path` through the
  same shared plugin-root reference scanner as `rewriteCommand`, so the isolated
  form is checked, legacy-rewritten, and refused when missing like the
  whole-word form. Verify the `hookpolicy` reference table, the catalog
  reference tests, and the `internal/materialize` quoted-token test pass
