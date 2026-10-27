## 1. Machine root resolution

- [ ] 1.1 Add a Claude Code machine-root helper beside the OpenCode one: absolute `CLAUDE_CONFIG_DIR` wins, a relative value returns `CLAUDE_CONFIG_DIR must be absolute`, unset or empty falls back to `<home>/.claude`.
- [ ] 1.2 Use the helper in the legacy install-root table (keep the default on an invalid value), `MachineRoot`, the legacy branch of destination resolution (report the error), and explicit-home detection (variable takes precedence over the supplied home).
- [ ] 1.3 Root the Claude `machine-mcp` aggregate destination at the resolved machine root instead of HOME so `.claude.json` follows the directory; leave repository scope and the unset layout unchanged.
- [ ] 1.4 Add the environment-variable name to the shared documented-variable allowlist in the docs contract test.

## 2. Safety of the relocated configuration file

- [ ] 2.1 Make the aggregate prune fence protect a scope-root aggregate's file whenever its resolved root names the same location as the default destination root.
- [ ] 2.2 Re-check every root-equality or scope-root-flag heuristic around Claude aggregates (native hook shared-route detection, existing-aggregate output-key revival, cleanup-link root selection) against the relocated layout and fix any that assume `.claude.json` is outside the root.
- [ ] 2.3 Key the Claude backup archive mapping on the scope-root aggregate so `.claude.json` stays an explicitly mapped entry in both layouts, and exclude mapped files from the ordinary relative paths so it is archived once.

## 3. Scope-overlap preflight

- [ ] 3.1 Generalize the Codex location-identity helpers (physical-root resolution through the nearest existing ancestor, ancestor test, case- and NFC-insensitive comparison) to target-neutral names without changing Codex behavior.
- [ ] 3.2 Add a Claude Code preflight, active only when `CLAUDE_CONFIG_DIR` is set and both Claude Code output keys are active, that refuses with `materialize.destination.overlap` naming both paths when the machine root and `<repo>/.claude` are the same location or one contains the other, drops both Claude Code keys from the run, and lets other targets continue. Run it before the first directory is created, beside the Codex and OpenCode preflights, and in read-only checking.

## 4. Test isolation

- [ ] 4.1 Unset `CLAUDE_CONFIG_DIR` in `testutil.IsolatedHome`.
- [ ] 4.2 Pin `CLAUDE_CONFIG_DIR` in the process-isolated command environment and its environment key lists, the in-process command `TestMain`, the E2E framework environment and sandboxed-key list, and the TUI QA isolated environment (`<home>/.claude`, as `CODEX_HOME` is pinned).
- [ ] 4.3 Re-initialize the legacy install-root table in the harness package `TestMain` after isolation so init-time roots do not leak a host value.

## 5. Tests binding every scenario

- [ ] 5.1 Harness unit tests: relocated roots for every destination, aggregate root, detection with and without the directory, relative-value error, unset/empty default — bind `claude-native-configuration/unset-or-empty-variable-keeps-the-default-roots` and `claude-native-configuration/harness-detection-recognizes-the-configuration-directory`.
- [ ] 5.2 Materialize tests with a relocated root: every kind in both materialization modes, `~/.claude` absent and `~/.claude.json` bytes unchanged, `.claude.json` unrelated members preserved, `sync --check` clean — bind `claude-native-configuration/machine-selections-install-under-the-configuration-directory` and `materialization/relocated-claude-code-machine-root-is-admitted`.
- [ ] 5.3 Materialize tests for the fence and reconciliation: prune with no MCP selected leaves `.claude.json` byte-identical; removing the last MCP server keeps the file; unmanaged collision fails closed while siblings install — bind `claude-native-configuration/pruning-the-relocated-root-preserves-the-configuration-file`, `claude-native-configuration/removing-the-last-mcp-server-keeps-the-configuration-file`, and `claude-native-configuration/unmanaged-collision-in-the-relocated-file-fails-closed`.
- [ ] 5.4 Materialize tests for refusal: relative value with machine selections (other targets and repository Claude Code still install), relative value with repository-only Claude Code, unsafe relocated root object — bind `claude-native-configuration/relative-value-refuses-machine-scope-work-before-any-write`, `claude-native-configuration/relative-value-does-not-affect-repository-only-output`, and `materialization/unsafe-relocated-claude-code-machine-root-is-refused`.
- [ ] 5.5 Materialize tests for overlap: `<repo>/.claude`, `<repo>`, a child of `<repo>/.claude`, case- and NFC-only spellings (existing and missing), and a disjoint directory — bind `claude-native-configuration/configuration-directory-inside-or-around-the-repository-root-refuses`, `claude-native-configuration/configuration-directory-that-aliases-the-repository-root-refuses`, and `claude-native-configuration/disjoint-configuration-directory-supports-both-scopes`.
- [ ] 5.6 Materialize or command test: output generated under `~/.claude` and `~/.claude.json` is untouched and unreported after the variable is set — bind `claude-native-configuration/default-location-output-is-left-in-place`.
- [ ] 5.7 Command tests: `status` and `doctor` root summaries show the relocated root and hard-error on a relative value; `backup` archives the relocated components and mapped `.claude.json`; `backup restore` publishes into the relocated root — bind `claude-native-configuration/status-doctor-and-backup-resolve-the-relocated-root` and `materialization/relative-claude-configuration-directory-is-a-hard-error-in-summaries`.
- [ ] 5.8 Command test: repository-scope selections with the variable set land in `<repo>/.claude` and `<repo>/.mcp.json` only — bind `claude-native-configuration/repository-scope-ignores-the-variable`.
- [ ] 5.9 E2E scenario reproducing issue #920 end to end with the built binary: isolated HOME, both `.claude.json` files present, `install --scope user`, `sync`, then hashes and directory listings; add it to the witnessed set so a skip fails the gate.
- [ ] 5.10 TUI QA journey `first-run-claude-config-dir.spec.ts`: variable points at a sibling of `~/.claude`, only that directory exists, the first-run dialog pre-selects Claude Code; annotate `claude-native-configuration/harness-detection-recognizes-the-configuration-directory` and list it in `tools/tui-qa/README.md`.

## 6. Documentation

- [ ] 6.1 `site/docs/reference/harnesses-and-output-paths.md`: Claude Code roots row, the Claude MCP exception paragraph, and a note under the Claude Code table that `~/.claude` and `~/.claude.json` read as `$CLAUDE_CONFIG_DIR` when set.
- [ ] 6.2 `site/docs/reference/environment-variables.md`: `CLAUDE_CONFIG_DIR` row (absolute only, machine scope only, `.claude.json` follows).
- [ ] 6.3 `AGENTS.md`: Claude aggregate bullet and the sandbox-variable sentence; `CODING_STANDARDS.md` process-sandbox paragraph.

## 7. Verification and delivery

- [ ] 7.1 `go run ./tools/specaudit trace` and `make spec-check` pass with every new scenario bound.
- [ ] 7.2 `make validate`, `make validate-e2e`, and `make tui-qa` pass; `go run ./tools/specaudit gate -base origin/main -head HEAD` passes.
- [ ] 7.3 Run the `open-code-review-delegate` review and address findings.
- [ ] 7.4 `openspec archive honor-claude-config-dir --yes` as the final commit of the pull request.
