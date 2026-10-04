## Implementation contract

Implement OpenCode as an opt-in native harness through the existing compilation,
materialization, ownership, inspection, and repair engines. The native gate uses
OpenCode 1.18.34. Active command hooks are supported; absent, disabled, or failed
plugins are documented as inactive. MCP/nested subagent hook coverage is not
advertised. Shared repository/machine custom destinations refuse before mutation.
These limits follow the implementation direction to deliver native agent support
without adding a separate enforcement or installation system. Existing generic
filesystem, ownership, acquisition, and repair requirements remain applicable.

## 1. Register and translate OpenCode

- [x] 1.1 Register the target, source ingester, compiler, native topology, installation adapter, and repair binding. Expose selection, conversion, status, and doctor through the existing registry; keep defaults unchanged.
- [x] 1.2 Detect `.opencode`, accept explicit native directories, resolve plural/singular aliases deterministically, and reject ambiguous distinct identities.
- [x] 1.3 Compile and ingest native skills, commands, subagents, and MCP servers. Preserve placeholders, selections, ordered/empty native permission fields, supported authentication, enabled state, and reference intent.
- [x] 1.4 Diagnose metadata loss and unsupported execution restrictions, rules, or executable plugins. Keep production source reads non-executing and bounded; retain valid independent resources.
- [x] 1.5 Extend independently authored native fixtures, round trips, and all eight-source/eight-target conversion matrices; retain compatibility of prior targets and formats.

## 2. Install native resources and shared configuration

- [x] 2.1 Resolve repository, XDG machine, JSON/JSONC, and separate absolute file/directory overrides. Refuse overlapping repository/machine destinations before writes.
- [x] 2.2 Bind both materialization modes to the existing destination and ownership machinery; preserve handwritten agent identities, unmanaged adapters, support boundaries, and incomplete installed generations.
- [x] 2.3 Reconcile MCP through the existing formatting-preserving durable ownership engine, including native trailing commas, collisions, malformed input, private creation modes, mandatory backups, no-ops, and journal recovery.
- [x] 2.4 Bind read-only checking, explicit backup inventory, and exact-acquisition repair. Cover byte-neutral adoption and interactive replacement without changing generic confirmation or policy rules.

## 3. Support native command hooks

- [x] 3.1 Emit a fixed dependency-free adapter and typed versioned JSON declarations outside plugin autoload paths. Ingest only recognized generated representations without importing source code.
- [x] 3.2 Support command `PreToolUse`, ordinary bare tool matchers, deterministic ordering, allow/block output, deadlines, and documented input/output/invocation limits. Reject unsupported forms visibly.
- [x] 3.3 Keep selected contributions additive in both modes. Copy the adapter at its installed location, preserve sibling hooks on removal, and prevent copying unselected sibling declarations as support.
- [x] 3.4 Execute the seven witnessed native tests: activation controls, compiled hooks, actual installed hooks/removal and native resource discovery, permission/plugin coexistence, handler/output boundaries, and invocation boundaries. Record verified runtime identity and coverage limits.

## 4. Verify consumers and finish

- [x] 4.1 Exercise repository/user collection installation, every selectable kind, both modes, removal, offline exact reuse, unchanged healthy locks, existing-target compatibility, repair, and non-execution sentinels through CLI E2E.
- [x] 4.2 Verify new native fields remain covered by resource assessment, redaction, and permitted inspection projection.
- [x] 4.3 Run a real-terminal OpenCode selection/install journey and the existing TUI journeys.
- [x] 4.4 Measure representative existing hot-path controls against the proposal revision and new OpenCode translation/ingestion/sync/check/repair benchmarks; retain raw results privately.
- [x] 4.5 Update user/library/native-path docs and the runtime protocol/version/limits note. Record the new harness support and intentional first-release limits in these change artifacts.
- [x] 4.6 Audit annotated scenario witnesses; run affected checks, `make validate`, `make spec-check`, and the combined E2E and TUI gates. Run the native gate separately.
- [x] 4.7 Privacy-screen the exact outgoing implementation, run OCR including test logic, report excluded files, fix confirmed findings, and complete a separate pre-push adversarial review.
- [x] 4.8 Commit the implementation, archive this change as the final commit, and push the branch with the user's author/committer attribution.
