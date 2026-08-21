---
name: agent-marketplace-development
description: >
  How to develop on the agent-marketplace codebase itself: architecture
  tour, testing workflow, TUI (Bubble Tea) and CLI (Cobra) conventions,
  and the config/resolver/materialization invariants that every change
  must preserve. Use when adding features, fixing bugs, or reviewing
  changes to the agent-marketplace Go binary.
metadata:
  tags:
    - development
    - go
    - tui
    - cli
    - agent-marketplace
---

# agent-marketplace-development

How to be productive working on the agent-marketplace Go binary.
This is about contributing to the tool itself, not about using it to
sync skills (that's the `agent-marketplace` skill next to this one).

## Orient Yourself First

Before touching anything, read these in order:

1. `AGENTS.md` — project charter, design decisions, session-completion rules.
2. The `agent-marketplace` skill next door — user-facing contract you
   must not break.

Then skim:

- `cmd/agent-marketplace/main.go` — Cobra root, subcommand wiring,
  TUI launch on bare TTY invocation.
- `internal/config/config.go` — data model (`Config`, `LocalConfig`,
  `RegistryEntry`).
- `internal/config/merge.go` — `Merge(global, repo)` and
  `resolveInheritedList` (the "specific wins over default" rule).
- `internal/resolver/resolver.go` — selects which resources are enabled.
- `internal/tui/state.go` — `Model` and `row` shape used by the TUI.

## Build, Test, Lint

```bash
make build           # CGO_ENABLED=0 go build -> ./agent-marketplace
make test            # go test ./...
make lint            # go mod tidy + go fmt + go vet + golangci-lint if present
make ci-lint         # non-destructive equivalent used in CI

# Scoped testing (faster iteration)
go test ./internal/config/...
go test ./internal/resolver/...
go test ./internal/tui/... -run 'TestToggle' -count=1 -v
go test ./cmd/agent-marketplace/...

# Build + run the TUI against the current repo for smoke testing
make build && ./agent-marketplace
```

Pre-commit runs `make test`, `make lint`, `make build`. If `go fmt`
modifies files during lint, re-stage and commit again.

## Architecture: Two Entry Points, One Data Flow

Both the CLI subcommands and the TUI drive the same pipeline:

```
load configs (global + repo + local)
        |
        v
  Merge(global, repo) -> *config.Config         [merge.go]
        |
        v
  load catalogs (clone/pull git, read local)    [cache, catalog]
        |
        v
  resolver.Resolve(cfg, catalogs, localCfg)     [resolver]
        |
        v
  materialize.Run (compile non-skills, link skills) [materialize]
```

- **CLI** (`cmd/agent-marketplace/`) runs this pipeline once, prints,
  exits.
- **TUI** (`internal/tui/`) runs it on startup and after every config
  write, keeping a `Model` in memory and re-rendering.

Everything below this line — resource selection rules, symlink targets,
name collision behavior — is shared. Fixes at the `internal/*` layer
fix both surfaces at once. Features that only make sense in one surface
belong in `cmd/agent-marketplace/` or `internal/tui/`.

## Materialization Invariants

`internal/materialize` is the authoritative v2 sync/drift engine. It
consumes `resolver.ResolvedSkill` values with attached IR resources,
compiles non-skill artifacts through `harness-compilation`, and writes
target roots for repo and machine scopes.

Preserve these invariants:

- **Skills are links, not copies.** Harness skill entries should be
  symlinks to files/directories under `~/.cache/agent-marketplace`, so
  target installs reflect cache state and bundled skill assets are not
  silently dropped.
- **Non-skills are compiled artifacts, unless a target declares
  otherwise.** Commands, subagents, hooks, and MCP servers are selected
  like skills and are normally materialized through HCC into the target
  harness' native files instead of skill symlinks. The exception is
  declared, never hard-coded: `harness.TargetInfo.SkillDirKinds` names
  the kinds that take the directory-symlink path instead. Devin sets it
  for `ir.KindCommand`, because a Devin command *is* a skill directory
  (`skills/<name>/SKILL.md` carrying `x-agent-marketplace-kind:
  command`), which also gives it the same prune coverage as a skill.
- **No harness-specific skill-link allowlists.** Decide whether a
  resource is a skill by the compiled/source artifact shape, the
  resource kind, and declarative `harness.TargetInfo` metadata — not by
  hard-coded target names like Claude, Factory, Cursor, Windsurf,
  Gemini, or Devin.
- **`AggregateFiles` must match what the compilers emit.**
  `harness.TargetInfo.AggregateFiles` is the single source of truth for
  which files a target merges into, per scope; both materialization
  routing and `pipeline.CollectManagedPaths` (safety backup coverage)
  read it. All six targets declare it, and the declaration is not
  editorial: it must equal "every compiled artifact with
  `MergeWithExisting == true`, at the output-root-relative path the
  planner writes it to, in that scope".
  `materialize.TestAggregateFilesMatchMaterializedMergeArtifacts`
  measures that set from real compilation and fails on any difference in
  either direction — never satisfy it by restating a list. An
  undeclared aggregate is a file mutated with no safety backup.
- **Shared aggregate files are ownership-reconciled, not
  marker-merged.** Targets that reconcile aggregates through ownership —
  currently only Devin, which declares
  `harness.TargetInfo.OwnershipReconciledAggregates` and is fenced out of
  the marker-strip prune walk by `aggregateProtectedPaths` on that field
  alone — go through `internal/ownership` plus
  `pkg/harness-compilation/jsonc`, in the sequence the `ownership`
  package doc names (`Open`, `Recover` once before any mutation,
  `Proof`, `Classify`, patch, `Apply`). Never inject a `_managed_by`
  marker into such a file and never infer ownership from its bytes:
  ownership is proven from durable state under
  `$XDG_STATE_HOME/agent-marketplace`, and missing or corrupt state must
  fail closed (every entry unmanaged; preserve everything, prune
  nothing).
- **Symlink-only targets refuse file mode.** A target that sets
  `harness.TargetInfo.SymlinkOnly` (Devin) rejects
  `config.MaterializationModeFile` with
  `materialize.devin.symlink-required` rather than degrading to regular
  files. Do not add a fallback. `SymlinkOnly` is about per-resource
  artifact shape only and is INDEPENDENT of
  `OwnershipReconciledAggregates`; Devin holding both is coincidence, so
  never use one as a proxy for the other.
- **Gemini is not a skill-link exception.** Gemini CLI discovers native
  Agent Skills from `.gemini/skills` and the generic `.agents/skills`
  alias; within a tier, `.agents/skills` takes precedence over
  `.gemini/skills`. Treat Gemini skill directories like other native
  skill-directory harnesses unless current vendor docs prove otherwise.
- **Keep pruning scoped.** Never run stale-file pruning at a repo root
  or home root. Split generated artifacts into safe component roots
  (for example `skills`, `commands`, `agents`, `hooks`, `.windsurf`)
  before pruning.
- **Cache namespaces matter.** If materialization creates derived cache
  entries under `~/.cache/agent-marketplace`, protect that namespace
  from `agent-marketplace gc` or installed symlinks can become broken.
- **Test one invariant across all targets.** When several beads ask for
  the same behavior per harness, write a table-driven regression test
  spanning all affected targets before implementing target-specific
  branches.

## Config Invariants You Must Not Break

These are load-bearing. Every PR must preserve them.

### Configuration-layer precedence (most → least specific)

1. `.agents/config.local.yaml` (gitignored personal overrides; only
   kind-specific resource lists and `exclude` are meaningful).
2. `.agents/config.yaml` (committed repo config).
3. `~/.agents/config.yaml` (user-personal machine-wide defaults).
4. `/etc/agent-marketplace/config.yaml` (externally managed organization
   policy; only this layer may set `required: true`).

### Attribute inheritance: specific wins over default

Inside a single config file, top-level resource lists (`skills`,
`commands`, `subagents`, `hooks`, `mcp`), `tags`, and `exclude` are
**defaults**. A `RegistryEntry` that sets its own resource lists,
`tags`, or `exclude` **replaces** (does NOT merge with) the top-level
for that entry. This is encoded in `resolveInheritedList`:

```go
func resolveInheritedList(defaults, overrides []string) []string {
    if overrides != nil {
        return append([]string(nil), overrides...)
    }
    // ... otherwise fall back to defaults
}
```

**Consequence for editors**: when writing a resource into a config, if
the matching registry entry has its own list for that kind, write into
*that* list, not the top-level. The TUI toggle modal
(`internal/tui/modal.go` → `runScopedToggle`) should use the generic
resource editor helpers (`AddResourceToRegistryInConfig`,
`DisableResourceInRegistryInConfig`, `AddResourceToConfig`) so skills,
commands, subagents, hooks, and MCP servers stay aligned.

### Required registries ignore local excludes

`required: true` organization registries sync all their resources to the
configured machine-scope harness roots regardless of repo/local config.
The resolver enforces this; never add a bypass.

### Executable resources need a deliberate selection

Hooks and MCP servers are only enabled by a name (`hooks`/`mcp` include, or a
collection named in a registry entry's `collections: include:`) or by
`required: true` organization policy. Broad selection — `default: true`, `tags:`,
`install --all` without `--kind`, `install --from` without a name, a top-level
`collections: include:`, and a collection reached by any of those — must never
enable them. When code has to withhold one, it reports it
(`resolver.ExecutableCollectionMemberWarning`,
`pipeline.InstallResult.Withheld`); dropping it silently is the bug this rule
exists to prevent. Never add a bypass, and never let a new code path enable an
executable kind without a selection that names it.

Two consequences are easy to get wrong:

- **Trust is keyed by (provider, collection name), and only an entry grants it.**
  `resolver.collectionTrust` is built from the config *before*
  `propagateTopLevelCollections` runs, because propagation deliberately copies
  top-level collection names onto every registry entry for *selection*. A
  top-level name grants nothing, however many registries publish it, so
  `config.MergeLayers` must not inherit it onto entries either (see
  `config.KindIncludeGrantsTrust`) and `catalog.LoadRegistries` has to carry the
  top-level selector into the config it returns for selection to keep working.
  Never key trust on a bare name.
- **Trust does not cross an `includes:` boundary.** `selectIncludedCollection`
  binds a nested name to the including collection's own registry first; when
  another registry answers the name, the artifact is re-attributed to that
  publisher and trust is re-evaluated for it (never inherited), so a collection
  the user vouched for cannot hand a name to a registry they did not.
- **Explicitness is per kind for hooks and MCP servers.** An include for a kind
  broad selection never reaches must not disable `default: true` for the other
  kinds (`resolver.hasExplicitSelection`), and the editor's
  `entryNodeAutoSelectsKind` must agree — a divergence makes the editor write an
  include the resolver reads as "and nothing else".
- **A command that reports an executable as installed must have written a
  selection for it.** Anything that only writes `default: true` has installed
  nothing of these two kinds.

### Name collisions refuse sync

If two registries expose the same resource kind and name, `Resolve`
returns an error. Resolve via per-registry kind-specific lists,
`exclude`, or removing a source. The same name may exist in different
kinds; CLI install/remove paths must ask for `--kind` when that makes a
name ambiguous. `cmd/install.go` mirrors collision behavior for
`install --all`.

### Config editing preserves comments

`internal/config/editor.go` edits YAML at the node level
(`yaml.Node` trees) rather than re-marshaling structs, so user
comments are preserved. When adding a new editor function, follow the
existing pattern:

- Read bytes, `parseConfigDocument`, ensure mapping, locate nodes,
  mutate content, `writeConfigDocument`.
- Helpers: `findMappingValue`, `findTopLevelSequence`,
  `findEntrySequence`, `removeMappingKey`, `ensureMappingSequence`.
- Idempotent for missing files: treat `os.ErrNotExist` as a no-op for
  remove operations.

## TUI Conventions (Bubble Tea)

The TUI is a standard [Elm-style][elm] Bubble Tea app:
`Init` → `Update(msg)` → `View()`. File map:

[elm]: https://github.com/charmbracelet/bubbletea

| File | Responsibility |
|---|---|
| `app.go` | `Model`, `Update`, `View`, top-level key handling |
| `state.go` | `Model` fields, `row`, `pane`, glyph/status logic |
| `actions.go` | Snapshot loading, catalog loading, tea.Cmd factories |
| `modal.go` | Per-scope toggle modal (`paneToggleModal`) |
| `tabs.go` | Scope / Registry / Status tab switching + per-tab state |
| `tree.go` | Section tree for the current tab |
| `flatitems.go` | Flattens the tree into the rendered list |
| `list.go` / `detail.go` | Left and right panes |
| `badges.go` | G/P/L/X origin badges next to each row |
| `footer.go` / `help.go` | Keybinding footer and help overlay |
| `overlay.go` | Centers the toggle modal on top of the base screen |
| `theme.go` | lipgloss styles |

### Message flow for a config-writing toggle

1. User presses `space` on a skill row → `paneToggleModal` opens with
   the current `row` captured as `m.modalSkill`.
2. User presses `g`/`p`/`l`/`e` → `scopedToggleAction` derives a
   `toggleAction` from the row's current flags and
   `runScopedToggle` returns a `tea.Cmd` that writes the config file.
3. Cmd emits `configWrittenMsg` (success or error).
4. `Update` handles `configWrittenMsg` by calling `loadSnapshot` to
   re-read configs, re-resolve, and re-render. Badges/glyphs update
   from the reloaded `row`.

**Don't** mutate `row` fields directly after a toggle — the source of
truth is the config file, and the snapshot reload is what produces new
rows. Mutating in place hides bugs where the write path didn't land in
the right config location.

### Tab state isolation

Each tab keeps its own cursor/offset/filter/fold state in
`tabStates[tab]` (`tabs.go`). `switchTab` saves outgoing state and
restores incoming state. Any new per-tab concern goes into
`tabViewState`, not the top-level `Model`.

### Refresh token pattern

Background catalog refreshes (`refreshLoadedMsg`) carry a
`refreshToken`. `Update` ignores results whose token doesn't match
`m.refreshToken`. This prevents a stale async git fetch from
overwriting a newer user-triggered reload. Preserve this pattern when
adding async work.

### UX for selecting skills across scopes and registries

The toggle modal is the only place in the TUI where the user picks
*where* a skill is enabled. The modal displays per-scope checks for
Global / Project / Local / LocalExclude and lets the user flip any of
them independently. When designing new flows:

- A skill can be enabled in multiple scopes simultaneously (e.g. both
  global and local). The rendered badge on the row (`badges.go`) shows
  the highest-precedence active scope; don't collapse the modal to a
  single-choice picker.
- The toggle always targets the specific registry entry that contains
  the skill (`row.RegistryURL`). A skill available from multiple
  registry entries is disambiguated by using the first entry in config
  order that exposes it (`buildRows` in `actions.go`).
- `required: true` skills refuse toggling via a `toastMsg`. Keep that
  guard — silently no-op'ing is confusing.
- `Available` rows (skill in a catalog but not resolved) are
  toggleable via the same modal. Don't split the UI into
  "install"/"toggle" — they're the same operation on different
  starting states.

### Adding a new keybinding

1. Add the key handler branch in `app.go handleKey` (or the
   appropriate `handle*Key` for a specific pane).
2. Update `help.go` and `footer.go` so the key is discoverable.
3. Write a test in `internal/tui/*_test.go` using `NewModel` and
   `Update(tea.KeyMsg{...})`. Mock file I/O with the `swap*` helpers in
   `toggle_flow_test.go` if the keybinding writes configs.

## CLI Conventions (Cobra)

Every subcommand lives in its own file in `cmd/agent-marketplace/` and
exposes `newFooCmd() *cobra.Command`. `main.go` registers them.

### Subcommand responsibilities

| Command | Writes config? | Triggers sync? |
|---|---|---|
| `list` | no | no |
| `install <name>` / `install --tag` / `install --all [--registry URL]` | yes (repo) | yes |
| `remove <name>` | yes (repo; resources or exclude) | yes |
| `sync` | no | yes |
| `update [--bump]` | sometimes (bump rewrites ref) | yes (via snapshot reload) |
| `status` | no | no |
| `gc` | no | no (cleans cache) |
| `tui` (or bare invocation on TTY) | yes (via modal) | on demand (`s`) |

### Shape of a good CLI command

- Validate args via `cobra.Command.Args`. Mutually exclusive flags
  (`install --tag` vs `<name>` vs `--all`) check in `Args`, not
  `RunE`, so `cobra` prints nice usage errors.
- `RunE` receives `cmd.OutOrStdout()` — use it, don't reach for
  `os.Stdout` directly. Tests rely on the writer injection.
- For config-writing commands: load configs → mutate config file via
  `internal/config` editors → call `runSync(io.Discard)` → print the
  result.
- Keep resource kind handling explicit. `--kind` accepts `skill`,
  `command`, `subagent`, `hook`, or `mcp`; without it, ambiguous
  same-name resources across kinds should error.
- Never reimplement selection logic. Call `config.Merge`, pass to
  `resolver.Resolve` with resource catalogs, consume the result.

### When to add a CLI command vs a TUI keybinding

- Both: flows that work well scripted (CI) *and* interactively
  (install, remove, sync, update).
- CLI only: deterministic one-shot actions, output-for-pipes,
  non-interactive environments (`--version`, `status`, `gc`).
- TUI only: exploratory flows that benefit from live state (browsing
  catalogs, fold/unfold, per-scope toggle modal). These don't need a
  CLI counterpart.

### Respect `AGENT_MARKETPLACE_NO_TUI`

Bare invocation on a TTY launches the TUI. The env var
`AGENT_MARKETPLACE_NO_TUI=1` and the hidden `--no-tui` flag force the
help path. Anything that might be invoked from CI must honor these
signals.

## Testing Playbook

### Unit tests

- `internal/config/editor_test.go` — round-trip YAML editing,
  comment preservation, idempotency.
- `internal/resolver/resolver_test.go` — selection rules, collisions,
  inheritance, local overrides.
- `internal/tui/*_test.go` — construct `Model` directly, feed
  `tea.KeyMsg` or `snapshotLoadedMsg`, assert on `m.rows`, rendered
  output via `renderList`, or glyph/badge helpers.

### End-to-end TUI tests (real file I/O)

`internal/tui/toggle_flow_test.go` exercises the full load →
edit → reload path against a temp repo. Use this pattern when a bug
"feels" like an integration issue (resolver-level, inheritance,
config edit → rendered state).

The key tools there:

- `swapCloneOrPull`, `swapDiscoverSkills`, `swapLoadGlobalConfig`,
  `swapLoadRepoConfig`, `swapLoadLocalConfig` — swap package-level
  function vars for deterministic test fixtures.
- `config.LoadConfigFromPath` / `LoadLocalConfigFromPath` — load from
  an explicit path so tests don't read `~/.agents/config.yaml`.

### Writing tests that exercise real merge + resolve

```go
repoRoot := t.TempDir()
// write .agents/config.yaml
restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
    return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
})
defer restoreRepo()
// ... mock catalogs via swapDiscoverSkills
snap := readSnapshot(repoRoot, homeDir, catalogLoadCached)
```

Never let tests reach the real `~/.agents/` or `~/.cache/` — always
mock via the swap helpers.

## Debugging Recipes

### "My toggle doesn't update the UI"

Check the data flow at three levels:

1. **Config file content** — did the edit actually land?
   `cat .agents/config.yaml` after the toggle.
2. **Resolver output** — is the skill in `resolved` with the right
   `IsGlobal`? Add a `TestXxx` that calls `resolver.Resolve` with the
   edited config directly.
3. **Row construction** — does `buildRows` produce a row with the
   expected `EnabledGlobal/Project/Local`?

The bug is almost always at layer 1 or 2. The TUI render is a pure
function of `rows` — if rows are right, render is right.

### "Catalog load takes forever on TUI startup"

`loadCatalogsForTUI` skips `cloneOrPull` when the cache is warm, then
triggers a background refresh (`refreshAfterLoad=true`). If startup is
slow, check that the warm-cache detection in that function still
matches the `skill.DiscoverSkills` expectations for your new registry
type.

### "Pre-commit hook fails on `make lint`"

`go fmt` modified files in place. Re-stage (`git add`) and commit
again. The hook is not buggy — it's telling you to commit formatted
code.

## What Not To Do

- Don't re-derive selection logic in the TUI or a new subcommand. Call
  `resolver.Resolve`.
- Don't edit YAML by re-marshaling `Config` structs. You'll drop
  comments. Use the `internal/config/editor.go` helpers or add a new
  one that follows the same node-level pattern.
- Don't add a CLI command that duplicates a TUI-only flow. Either
  factor the shared logic into `internal/` first, or keep it
  TUI-only.
- Don't introduce blocking git fetches on the render path. Catalog
  refreshes run as `tea.Cmd` and emit `refreshLoadedMsg`.
- Don't silently succeed when a config write targets the wrong list.
  Cover each scope with a test that verifies the intended list received
  the write (see `TestReadSnapshotProjectToggleWithPerEntrySkills`).
