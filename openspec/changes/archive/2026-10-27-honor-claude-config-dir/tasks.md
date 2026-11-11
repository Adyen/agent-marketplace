## 1. Machine root resolution

- [x] 1.1 Add a Claude Code machine-root helper beside the OpenCode one: absolute `CLAUDE_CONFIG_DIR` wins, a relative value returns `CLAUDE_CONFIG_DIR must be absolute`, unset or empty falls back to `<home>/.claude`.
- [x] 1.2 Use the helper in the legacy install-root table (keep the default on an invalid value), `MachineRoot`, the legacy branch of destination resolution (report the error), and explicit-home detection (variable takes precedence over the supplied home).
- [x] 1.3 Root the Claude `machine-mcp` aggregate destination at the resolved machine root instead of HOME so `.claude.json` follows the directory; leave repository scope and the unset layout unchanged.
- [x] 1.4 Add the environment-variable name to the shared documented-variable allowlist in the docs contract test.

## 2. Safety of the relocated configuration file

- [x] 2.1 Make the aggregate prune fence protect a scope-root aggregate's file whenever its resolved root names the same location as the default destination root.
- [x] 2.2 Re-check every root-equality or scope-root-flag heuristic around Claude aggregates (native hook shared-route detection, existing-aggregate output-key revival, cleanup-link root selection) against the relocated layout and fix any that assume `.claude.json` is outside the root.
- [x] 2.3 Key the Claude backup archive mapping on the scope-root aggregate so `.claude.json` stays an explicitly mapped entry in both layouts, and exclude mapped files from the ordinary relative paths so it is archived once.

## 3. Scope-overlap preflight

- [x] 3.1 Generalize the Codex location-identity helpers (physical-root resolution through the nearest existing ancestor, ancestor test, case- and NFC-insensitive comparison) to target-neutral names without changing Codex behavior.
- [x] 3.2 Add a Claude Code preflight, active only when `CLAUDE_CONFIG_DIR` is set and both Claude Code output keys are active, that refuses with `materialize.destination.overlap` naming both paths when the machine root and `<repo>/.claude` are the same location or one contains the other, drops both Claude Code keys from the run, and lets other targets continue. Run it before the first directory is created, beside the Codex and OpenCode preflights, and in read-only checking.

## 4. Test isolation

- [x] 4.1 Unset `CLAUDE_CONFIG_DIR` in `testutil.IsolatedHome`.
- [x] 4.2 Neutralize `CLAUDE_CONFIG_DIR` in the command, E2E and TUI environments, and prove that the command runner strips a poisoned inherited value. Use empty values, not `<home>/.claude`, to retain the default MCP layout.
- [x] 4.3 Verify that the existing harness `TestMain` re-initializes the legacy install-root table after isolation so init-time roots do not leak a host value.

## 5. Tests binding every scenario

- [x] 5.1 Harness unit tests: relocated destinations, aggregate root, detection with and without the directory, relative refusal, and default roots; bind both default-root and harness-detection scenarios.
- [x] 5.2 Materialize lifecycle matrix: every resource kind, both modes, existing/missing configuration, permissions, independent default account and a clean drift check; bind machine installation and materialization root admission.
- [x] 5.3 Reconciliation and a non-vacuous prune-fence regression: handwritten bytes and metadata survive stale-resource pruning, last MCP removal retains the file, malformed/unmanaged collisions preserve account state while a sibling installs.
- [x] 5.4 Refusal matrix: relative machine root, repository-only output with an invalid machine setting, unrelated targets and unsafe file/dangling/cyclic roots; bind the three refusal scenarios.
- [x] 5.5 Overlap matrix: equal, ancestor, child, case/NFC spellings, symlink aliases, existing/missing locations and disjoint roots inside/outside the repository; bind both overlap scenarios and disjoint scopes.
- [x] 5.6 Preserve default output after switching accounts, including relocation to HOME, its parent or an admitted alias; keep inactive output out of the relocated drift report.
- [x] 5.7 Command tests: relocated status/doctor, relative summary/create/restore refusal, backup/restore, MCP removal and unchanged default state; pipeline tests prove exactly-once archive membership and restoration across both layouts.
- [x] 5.8 Command test: repository-only skill and MCP output ignores `CLAUDE_CONFIG_DIR`; bind the repository-scope scenario.
- [x] 5.9 Witnessed E2E reproduction with both account files, user install/sync/check/update/removal, byte comparisons and destination assertions.
- [x] 5.10 Real-terminal `first-run-claude-config-dir.spec.ts` journey proves detection and saving of the relocated account; bind the detection scenario and document the journey.

## 6. Documentation

- [x] 6.1 Document relocated roots, MCP paths, overlap refusal and unchanged repository scope in `harnesses-and-output-paths.md`.
- [x] 6.2 Add the `CLAUDE_CONFIG_DIR` row to `environment-variables.md`.
- [x] 6.3 Update `AGENTS.md` and `CODING_STANDARDS.md` with effective destinations and sandbox isolation.

## 7. Verification and delivery

- [x] 7.1 `go run ./tools/specaudit trace` and `make spec-check` pass with every new scenario bound.
- [x] 7.2 `make validate` and the final fresh `make validate-full` pass, including witnessed root tests, E2E/configuration compatibility, lint/build, shellcheck and all 86 terminal journeys; committed `specaudit gate` passes.
- [x] 7.3 Run the `open-code-review-delegate` review and address findings.
- [x] 7.4 Archive `honor-claude-config-dir` and publish its canonical specifications in the final commit of the pull request.
