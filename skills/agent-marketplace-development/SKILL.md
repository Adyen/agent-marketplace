---
name: agent-marketplace-development
description: >
  How to develop on the agent-marketplace codebase itself: architecture
  tour, testing workflow, TUI (Bubble Tea) and CLI (Cobra) conventions,
  and the config/resolver/materialization invariants that every change
  must preserve. Use when adding features, fixing bugs, or reviewing
  changes to the agent-marketplace Go binary, running local TUI QA,
  or maintaining harness compatibility validation.
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

1. `AGENTS.md` — spec authority, build/test/validation gates, command-test
   isolation, session-completion rules. It owns the Make targets and test
   selection rules; this skill does not repeat them.
2. `openspec/specs/` — the normative behavior. A behavior change needs an
   OpenSpec change with a spec delta, and every scenario needs a
   `// spec: <capability>/<scenario>` test binding.
3. `CODING_STANDARDS.md` — design, security-boundary, testing, and logging
   requirements.
4. The `agent-marketplace` skill next door — the user-facing contract.

Then skim:

- `cmd/agent-marketplace/main.go` — Cobra root, subcommand wiring,
  TUI launch on bare TTY invocation.
- `internal/config/v4.go` — the strict v4 `sources`/`resources` model
  (`V4Config`) and its runtime adapter into the internal
  `Config`/`RegistryEntry` model the resolver consumes. The adapter is not
  legacy consumer-format support.
- `internal/config/effective_config.go` — `LoadEffective` loads the four
  layers (and their includes and locks).
- `internal/config/merge.go` — `MergeLayers` and `resolveInheritedList`
  (runtime requirement inheritance; not an on-disk consumer schema).
- `internal/pipeline/pipeline.go` — `Resolve`, `ResolveWithExactLockLease`,
  and `Sync`: the one pipeline both CLI and TUI call.
- `internal/resolver/resolver.go` — selects which resources are enabled.
- `internal/harness/roots.go`, `registry.go`, `destinations.go` —
  `TargetInfo` metadata, the Devin/Codex installation adapters, and
  single-root vs. Codex-partitioned destinations.
- `internal/tui/state.go` — `Model` and `row` shape used by the TUI.

## Build and smoke test

Use the Make targets and scoped `TEST_PACKAGES` invocations in `AGENTS.md`.
To smoke-test the TUI against the current checkout:

```bash
make build && ./agent-marketplace
```

A smoke launch or Go test run does not exercise the real-terminal journeys;
for user-interaction changes, follow
[Local TUI QA](#local-tui-qa-real-terminal) below.

Pre-commit (`.pre-commit-config.yaml`) runs `make test`, `make lint`, and
`make build`. `make lint` rewrites files (`go mod tidy`, `go fmt`); re-stage
and commit again when it does.

## Architecture: Two Entry Points, One Data Flow

Both the CLI subcommands and the TUI drive `internal/pipeline`:

```
pipeline.LoadEffective -> config.EffectiveConfig    [four layers + includes + locks;
        |                                            v4 adapted into runtime requirements]
        v
  config.MergeLayers(layers) -> *config.Config      [merge.go]
        |
        v
  catalog.LoadRegistriesWithContext                 [exact Git snapshots, live dirs]
        |
        v
  resolver.Resolve(cfg, catalogs, localCfg,
      WithResources, WithTargetHarnesses, ...)      [resolver]
        |
        v
  pipeline.Sync: backup.Create, materialize.Run     [compile, link/copy, reconcile]
```

- **CLI** (`cmd/agent-marketplace/`) runs this pipeline once, prints,
  exits. Mutating commands (`install`, `remove`, `update`) go through
  `pipeline.Install`/`Remove`/`Update`, which publish config and the layer
  lock in one dependency transaction (`internal/dependencytxn`).
- **TUI** (`internal/tui/`) runs it on startup and after every config
  write, keeping a `Model` in memory and re-rendering.

Everything below this line — resource selection rules, symlink targets,
name collision behavior — is shared. Fixes at the `internal/*` layer
fix both surfaces at once. Features that only make sense in one surface
belong in `cmd/agent-marketplace/` or `internal/tui/`.

### Resolver invariants

- `resolver.Resolve` may receive catalog entries whose `Resource` is nil through
  legacy discovery. When the caller has the corresponding IR map, pass it via
  `resolver.WithResources`; resolution attaches those resources before nested
  collection expansion so materialization does not lose the definition.
- Nested collection ownership is selected from configured registry metadata,
  not catalog/cache-key iteration order. Required policy and config
  source/file precedence win; ties prefer the containing URL/ref/path and then
  stable URL/path ordering. Registry subpaths are slash-separated artifact
  paths: equivalent spellings (such as `./foo/`, repeated separators, or
  backslashes) share one identity, while config editors retain authored
  URL/path spelling.
- Nested collection harnesses inherit the containing collection's effective
  targets, and an explicitly configured nested list is intersected with them.
  A nested owner can narrow but never widen its parent; explicit empty remains
  an opt-out. Include ancestry is branch-local, so shared DAG nodes are valid
  while an ancestor repeated on one branch is a cycle.

## Materialization Invariants

`internal/materialize` is the authoritative sync/drift engine. It
consumes `resolver.ResolvedArtifact` values with attached IR resources,
compiles non-skill artifacts through `harness-compilation`, and writes
target roots for repo and machine scopes.

Preserve these invariants:

- **Skills link by default; file mode copies.** Default-mode harness skill
  entries are symlinks to files/directories under
  `~/.cache/agent-marketplace`. File mode writes regular files/trees for
  every target through the generic materializer, including Devin regular
  skill/command trees and subagent files; aggregate ownership reconciliation
  remains independent of installation mode. `TargetInfo.FileCopyKinds` is the
  one declared exception (Factory commands are always copied because the
  Droid CLI does not follow symlinked command files).
- **Non-skills are compiled artifacts, unless a target declares
  otherwise.** Commands, subagents, hooks, and MCP servers are selected
  like skills and are normally materialized through HCC into the target
  harness' native files instead of skill symlinks. The exception is
  declared, never hard-coded: `harness.TargetInfo.SkillDirKinds` names
  the kinds that take the skill-directory path instead. Devin and Codex set it
  for `ir.KindCommand`, because their commands are skill directories containing
  `SKILL.md` with `x-agent-marketplace-kind: command`. Default mode links those
  directories; file mode writes them as regular trees for both targets
  through the generic materializer.
- **No harness-specific skill-link allowlists.** Decide whether a
  resource is a skill by the compiled/source artifact shape, the
  resource kind, and declarative `harness.TargetInfo` metadata — not by
  hard-coded target names like Claude, Factory, Cursor, Windsurf,
  Gemini, Devin, or Codex.
- **`AggregateFiles` must match what the compilers emit.**
  `harness.TargetInfo.AggregateFiles` is the single declaration both
  materialization routing and `pipeline.CollectManagedPaths` (safety
  backup coverage) read for which files a target merges into, per
  scope. Codex, Devin, and Claude derive the set from their registered
  native-layout descriptor in
  `pkg/harness-compilation/target/native_layout.go`, the topology
  authority; the remaining targets declare it literally in
  `internal/harness/roots.go`. Either way the value is not editorial:
  it must equal "every compiled artifact with
  `MergeWithExisting == true`, at the output-root-relative path the
  planner writes it to, in that scope".
  `materialize.TestAggregateFilesMatchMaterializedMergeArtifacts`
  measures that set from real compilation and fails on any difference in
  either direction — never satisfy it by restating a list. An
  undeclared aggregate is a file mutated with no safety backup.
- **Shared aggregate files use an explicit reconciliation policy.**
  Marker-reconciled targets use their declared marker metadata and remain
  eligible for marker-based stale-entry pruning. Devin and Codex instead
  declare `harness.TargetInfo.OwnershipReconciledAggregates` and are fenced
  out of the marker-strip prune walk by `aggregateProtectedPaths` on that
  field alone. They go through `internal/ownership` plus their
  format-preserving JSONC/TOML patchers, in the sequence the `ownership`
  package doc names (`Open`, `Recover` once before any mutation, `Proof`,
  `Classify`, patch, `Apply`). Never inject a `_managed_by` marker into an
  externally reconciled file and never infer its ownership from bytes:
  ownership is proven from durable state under
  `$XDG_STATE_HOME/agent-marketplace`, and missing or corrupt state must
  fail closed (every entry unmanaged; preserve everything, prune nothing).
  Gemini MCP servers in `settings.json` use the same ownership store
  (`internal/materialize/gemini_native.go`) while Gemini hooks keep markers;
  aggregates installed outside the target root (Claude `.mcp.json` /
  `~/.claude.json`, Gemini `settings.json`, Windsurf hooks) are routed by
  `nativeAggregateInstalls` in `internal/harness/native_aggregates.go`.
- **Codex has two resolved destinations per scope.** Repo output splits
  between `<repo>/.agents` and `<repo>/.codex`; machine output splits
  between `~/.agents` and an absolute `$CODEX_HOME` (default `~/.codex`).
  Route artifacts by their `.agents`/`.codex` prefix through
  `ResolveDestinations`; never flatten Codex into one root or let a repo
  symlink escape the repository. `.agents/skills` alone is not Codex
  installation evidence.
- **Installation mode is generic distribution policy.** Every target uses
  the same file-copy or symlink materializer, selected by configuration.
  Compilers describe native artifact paths and contents, not installation
  mode. Shared-entry reconciliation remains independent: never infer
  `OwnershipReconciledAggregates` from how resource files are installed.
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
- **Test one invariant across all targets.** When the same behavior applies to
  several harnesses, write a table-driven regression test spanning all affected
  targets before adding target-specific branches.

## Config Invariants You Must Not Break

These are load-bearing. Every PR must preserve them.

### Four-layer precedence (most → least specific)

1. `.agents/config.local.yaml` (gitignored personal overrides; no
   `harnesses` or allowlists).
2. `.agents/config.yaml` (committed repo config).
3. `~/.agents/config.yaml` (user-personal machine config).
4. `/etc/agent-marketplace/config.yaml` (organization-managed machine config;
   root-owned trust rules; `required: true` groups are org-mandatory).

Each layer owns a lock next to it (`lock.yaml`, `lock.local.yaml`). Every
existing consumer root, including organization and personal-local, must declare
exact `apiVersion: agent-marketplace/v4`. An omitted root version fails with
`v4.api-version-required`; an explicit unsupported/invalid value fails with
`v4.api-version`, identifying the authored file and `apiVersion` field. An
existing empty/versionless file is not an absent layer. Explicitly included
fragments under a validated v4 root may omit the version or declare exact v4;
this does not relax include authority, source binding, or layer restrictions
(`v4.layer-field`).

Validate all required roots/includes before new acquisition, config/lock staging
or publication, or harness writes/prunes, including direct editors and ad-hoc
paths. Refusal preserves bytes/metadata and creates no missing writable root or
lock. Ordinary logs and established safe replay of a pre-existing dependency
journal are separate: read-only dependency modes refuse with
`dependency.recovery-required`, while recovery-capable commands may finish
already-admitted writes before fresh validation. Do not use recovery as a new
legacy decoder.

There is no shipped `migrate`, startup conversion prompt, TUI migration modal,
or consumer legacy reader/editor. Users convert writable roots and include
trees with an older released migrator before upgrading; administrators convert
organization configuration externally. Conversion can refuse includes,
unrepresentable input, or missing verified Marketplace evidence; changing only
the sentinel is insufficient. Keep lock v1 reads/v2 writes, historical
source-revision verification, operator `agent-marketplace.io/marketplace/v1`,
API/artifact/native schemas, ownership state, and journal formats independent.

### Source-bound groups and the runtime adapter

V4 resource groups belong to named sources declared by the same root/include
tree. Preserve ordered include/exclude decisions, fragment/root precedence,
relative-directory bases, source bindings, and layer provenance. The v4 adapter
emits the runtime requirements used by catalog, resolver, and materialization;
that internal model is not a legacy on-disk format or a serialization target.
Do not remove shared runtime inheritance/selection logic just because its
historical types have registry-entry names.

**Consequence for editors**: write source-bound selections into the selected
owning root's `sources`/`resources`, never old top-level lists or an included
fragment. The TUI toggle modal (`internal/tui/modal.go` → `runScopedToggle`)
runs inside the TUI's `MutateConfigWithDependencies` hook
(`cmd/agent-marketplace/tui.go`) and edits the staged path through
`pipeline.EditLayerAtPath` with a `pipeline.LayerMutation`. Preserve the original
declaring path for staged reloads; every kind and scope uses the v4 editor.

### Required selections and machine scope ignore local excludes

`required: true` on a v4 resource group protects its selected resources from
exclusion and removal; it selects nothing by itself. Selections come from
`include`, `tags`, or `all` where the source kind supports them. For Git and
directory sources, `all: true` explicitly selects every resource; adding
`required: true` makes those selections mandatory. Unsupported hosted dynamic
selectors remain refused.

Organization/user selections install into their configured machine-scope
harness roots. Repo and checkout-local excludes apply only to non-required
repo-scoped resources; they cannot hide machine-scoped resources or modify
another scope's selections or ownership. The resolver enforces these boundaries;
never add a bypass.

### Name collisions resolve deterministically

If two registries expose the same resource kind and name, `Resolve` selects a
winner and returns a soft warning; it does not fail sync. It chooses by
`required: true`, config source/file precedence, then alphabetically first
normalized URL; complete ties use stable config order. Warnings list every
registry/path participant, including same-registry paths deduplicated for
materialization. When the same URL/path is present at multiple refs, labels also
include `@<ref>`; remediation strips that display-only suffix and uses the URL,
`--path`, and `--ref` when needed. Use per-registry kind-specific lists, `exclude`, or
removal of the current winner to change the result. Same URL+ref parent/child
paths are merged as one physical source and the deeper path wins an overlap;
cross-scope overlaps remain complementary. The same name may exist in different
kinds; CLI install/remove paths must ask for `--kind` when needed.
Hooks are the exception to single-winner collisions: hooks from independent
sources all install. `cmd/agent-marketplace/install.go` mirrors the collision
behavior for `install --all`.

### Config editing preserves comments

Config editors edit YAML at the node level (`yaml.Node` trees) rather
than re-marshaling structs, so user comments are preserved:

- `internal/config/layer_editor.go` (`OpenLayerEditor`) and `v4_editor.go`
  are the v4-aware layer editors; callers reach them through
  `pipeline.EditLayerAtPath`. They refuse edits owned by an included
  fragment (`config.edit.include-owned`) and change nothing on refusal.
- Keep shared YAML node helpers only where v4 edits use them. Do not restore
  legacy reader/writer branches or serialize the internal runtime model back
  into consumer files. Ref edits, including `update --bump`, target eligible
  v4 sources owned by the root; included-fragment refs stay unchanged.
- Follow the same shape for new editors: read bytes, parse, ensure the
  mapping, locate nodes, mutate, write. Treat `os.ErrNotExist` as a no-op for
  remove operations.

## TUI Conventions (Bubble Tea)

The TUI is a standard [Elm-style][elm] Bubble Tea app:
`Init` → `Update(msg)` → `View()`. File map:

[elm]: https://github.com/charmbracelet/bubbletea

| File | Responsibility |
|---|---|
| `app.go` | `NewModel`, `Update`, `View`, `handleKey` |
| `keys.go` | `keyMap`: every binding and its help text |
| `state.go` | `Model` fields, `row`, `pane`, glyph/status logic |
| `actions.go` | Snapshot loading (`loadSnapshot`/`readSnapshot`), `buildRows`, tea.Cmd factories |
| `requests.go` | `requestGeneration` bookkeeping for async results |
| `modal.go` | Per-scope toggle modal (`paneToggleModal`) |
| `bulk.go` | Section bulk-toggle modal (`space` on a section header): scope → direction → confirmation, one staged layer transaction |
| `view_settings.go` | `v` View settings modal (primary/secondary grouping with live preview) and the versioned per-user preference file under `$XDG_STATE_HOME` |
| `harness.go` / `init.go` | `H` harness modal and first-run harness selection |
| `edit_config.go` / `editor_process.go` | `E` config picker and `$EDITOR` lifecycle |
| `discovery.go` | Hosted Marketplace discovery rows |
| `diagnostics.go` | `e` errors/warnings view |
| `bundle_tree*.go` | `enter` file browser for cached resource files |
| `tree.go` / `flatitems.go` | Section tree and its flattened rendered list |
| `list.go` / `detail.go` | Left and right panes |
| `badges.go` | `G`/`P`/`L`/`R`/`P-` origin badges next to each row |
| `footer.go` / `help.go` | Footer hint line and `?` overlay, rendered from `keyMap` |
| `overlay.go` | Centers modals on top of the base screen |
| `sanitize.go` / `terminal*.go` | Untrusted-text sanitizing, terminal size and restore guard |
| `theme.go` | lipgloss styles |

`docs/tui-product-contract.md` describes the intended TUI behavior; the
`tui-*` capabilities under `openspec/specs/` are normative.

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

### Request generation pattern

Async results (`snapshotLoadedMsg`, `refreshLoadedMsg`, detail and
Marketplace discovery loads) carry a `requestGeneration` issued by
`m.beginRequest(kind)`. `Update` drops a result unless
`m.acceptsRequest(kind, msg.generation)` matches the active generation for
that kind (`requests.go`). This prevents a stale background fetch from
overwriting a newer user-triggered reload. New async work gets a
`requestKind` and follows the same pattern.

### UX for selecting skills across scopes and registries

The toggle modal is the only place in the TUI where the user picks
*where* a single skill is enabled; `space` on a section header opens the
section bulk-toggle modal (`bulk.go`), which applies the same per-row
eligibility to every matching row in one staged transaction after an
explicit scope, direction, and confirmation. The single-row modal displays
per-scope checks for
Global / Project / Local / LocalExclude and lets the user flip any of
them independently. When designing new flows:

- A skill can be enabled in multiple scopes simultaneously (e.g. both
  global and local). The letter badge on the row (`badges.go`) shows
  every active scope (`GPL`, `P-`, ...); don't collapse the modal to a
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

1. Add a `key.Binding` with help text to `keyMap` in `keys.go`; the footer
   and `?` overlay render from it.
2. Add the handler branch in `app.go` `handleKey` (or the pane-specific
   handler it dispatches to).
3. Write a test in `internal/tui/*_test.go` using `NewModel` and
   `Update(tea.KeyMsg{...})`. Mock file I/O with the `swap*` helpers in
   `toggle_flow_test.go` if the keybinding writes configs.
4. Update `site/docs/reference/tui.md`, and add or extend a journey under
   `tools/tui-qa` when the key changes a user-visible flow.

## CLI Conventions (Cobra)

Each subcommand exposes `newFooCmd() *cobra.Command`, usually in its own
file in `cmd/agent-marketplace/` (`newRemoveCmd` lives in `main.go`).
`main.go` registers them. A new command or flag needs a behavioral test
witness, or `TestPublicCommandPathsHaveBehaviorTests` fails; new command
tests open with the `isolateCommandTest` guard described in `AGENTS.md`.

### Subcommand responsibilities

| Command | Writes dependency state? | Triggers sync? |
|---|---|---|
| `init` | new v4 user config only | no |
| `list` / `search` | no | no |
| `install <name>` / `--tag` / `--all` / `--from` / Marketplace locator | config and its layer lock in one transaction | yes |
| `remove <name>` | yes, transactionally with its layer lock | yes |
| `lock [--check\|--upgrade]` | generated locks unless checking | no |
| `sync` | stale writable locks unless `--locked`/`--check`/`--offline` | yes |
| `update [--bump]` | locks; config too when bump rewrites an eligible v4 source ref | yes (from exact snapshots) |
| `status` / `doctor` | no (`doctor --repair` adopts ownership state) | no |
| `gc` | no | no (cleans only entries inactive in validated config and locks) |
| `backup`, `deinit`, `telemetry`, `convert` | own state only | no |
| `serve` | Marketplace operator state | no |
| `tui` (or bare invocation on TTY) | config and its layer lock for mutations | on demand (`s`) |

`remove` keeps machine- and repo-scoped occurrences distinct. If both copies
are enabled, an unscoped same-kind removal is ambiguous; an explicit
`--scope user`, `--scope repo`, or `--scope local` selects the corresponding
occurrence without suppressing the others. Repo/local scopes must not be used
to suppress machine resources, and required resources are never removable.
For `remove --from`, `--scope` selects the config root (repo by default),
`--path` narrows URL matching to an entry/subtree, and any `@ref` in a
collision label is diagnostic text rather than part of the URL.

### Shape of a good CLI command

- Validate args via `cobra.Command.Args`. Mutually exclusive flags
  (`install --tag` vs `<name>` vs `--all`) check in `Args`, not
  `RunE`, so `cobra` prints nice usage errors.
- `RunE` receives `cmd.OutOrStdout()` — use it, don't reach for
  `os.Stdout` directly. Tests rely on the writer injection.
- For config-writing commands, call the `internal/pipeline` operation
  (`Install`, `Remove`, `Update`) or wrap a layer edit in
  `pipeline.MutateLayer` / `MutateConfigWithDependencies`, so the config and
  its layer lock are staged and published in one dependency transaction, then
  print the result.
- Keep resource kind handling explicit. `--kind` accepts `skill`,
  `command`, `subagent`, `hook`, or `mcp` (plus `collection` for `list`,
  `search`, and `remove`); without it, ambiguous same-name resources across
  kinds should error.
- Never reimplement selection logic. Go through `pipeline.Resolve` (which
  loads effective config, merges layers, and calls `resolver.Resolve`) and
  consume the result.

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

### Consumer launch and interoperability gates

Successful consumer command/flag coverage is v4-only, requiring exact v4 in
every present root including organization; schema-independent commands retain
successful configuration-free witnesses. Refusals, relabeled legacy records,
and help-only runs do not count as operational success. Keep the gaps allowlist
empty and port functional selection tests rather than deleting their assertions.

The normal Marketplace E2E gate witnesses unsupported/versionless roots in
every layer and explicit non-v4 fragments, with no-new-work preservation and
acquisition counters. Pre-existing journal recovery is asserted separately;
crash tests interrupt supported v4 mutations, not consumer migration. The
pre-v4 success `upgrade-gate` and CI job are retired; preserve independent lock,
source-revision, scope, collision, and ownership evidence in v4 fixtures.

Keep `released-client-gate` pinned to checksum-verified stable v4.1.3 and a
shared v4 repository fixture, with separate actors and healthy lock-byte/native
hook/owned-entry checks. After successful planning, aggregate `E2E Gates` must
require both Marketplace/released-client jobs to succeed when selected or both
to intentionally skip when unselected; missing witnesses, failure, cancellation,
and unexpected skips fail acceptance. See `AGENTS.md` for commands/isolation.

### Harness validation and CI (on demand)

Use this section only when adding a harness, changing native resource
compilation/materialization, or investigating the `Harness Compatibility`
check. Ordinary CLI/TUI work does not require loading the CI guides.

From the agent-marketplace checkout, read `docs/harness-validation/README.md`,
then only the relevant harness guide in that directory. Resolve these paths
from the checkout, not from an installed or cached copy of this skill.
The guide owns the probe contracts, coverage gaps, image/release policy,
required merge gate, fixture trust boundaries, and artifact diagnostics;
do not duplicate that policy here or in always-loaded project guidance.

For local verification, `make validate-e2e` runs every configuration
compatibility scenario together with E2E; run `make harness-compat-check` or
`make harness-compat-config` separately only when debugging those gates.
None of them invokes vendor CLIs. Native executables
and containers run only in GitHub Actions, with no model credentials or
model turns; synchronization must never grant real workspace trust.

### Local TUI QA (real terminal)

Use this workflow to verify user interactions through a real PTY, separately
from the Go model/file-I/O tests below and the CLI suite run by `make e2e`.
Run commands from the repository root on macOS or Linux; Docker, direnv,
and organization-specific infrastructure are not required.

1. **Prepare.** Make Go, Git, Make, Node.js, and pnpm available. Use
   [`tools/tui-qa/.node-version`](../../tools/tui-qa/.node-version) for the
   Node pin, or a version allowed by `engines.node` in
   [`package.json`](../../tools/tui-qa/package.json); match its
   `packageManager` pnpm version exactly. Install the locked dependencies:

   ```sh
   pnpm --dir tools/tui-qa install --frozen-lockfile
   ```

   Continue when installation succeeds with the tracked lockfile unchanged.

2. **Preflight.** Typecheck the tests and check that the PTY runner loads:

   ```sh
   make tui-qa-check
   ```

   Continue when the command exits zero. This checks setup, not journeys.

3. **Run.** Execute the complete suite:

   ```sh
   make tui-qa
   ```

   This repeats preflight, builds the dedicated `e2e`-tagged binary, and runs
   the Node process tests and PTY journeys. The tag redirects organization
   config into the sandbox; a normal `make build` binary is not a substitute.
   Fixtures supply temporary HOME/XDG roots, workspaces, and local Git
   registries, keeping test mutations separate from personal configuration.

4. **Report.** QA passes when `make tui-qa` exits zero with both runners
   passing and every listed journey exercised, none skipped. On failure, retain the failing
   test name and console output/current terminal screen; the suite produces
   no snapshot or trace files. Report setup failures as blocked, not passed.

   When a journey asserts the outcome of a config write, wait for the
   authoritative post-reload assessment (for example the `~ N out of sync`
   drift state) before quitting: a `config written` toast acknowledges the
   write, not the settled drift verdict.

For journey coverage or an already-built test binary, read the
[TUI QA package reference](../../tools/tui-qa/README.md).

### Unit tests

- `internal/config/editor_test.go`, `layer_editor_test.go` — v4 YAML
  editing, comment preservation, include ownership, and idempotency; retain
  direct-editor unsupported-format refusals and valid controls.
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

- `swapCloneOrPull`, `swapDiscoverSkills` (`actions_test.go`) and
  `swapLoadOrgConfig`, `swapLoadGlobalConfig`, `swapLoadRepoConfig`,
  `swapLoadLocalConfig` (`toggle_flow_test.go`) — swap package-level
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
// ... swapLoadOrgConfig/swapLoadGlobalConfig, mock catalogs via swapDiscoverSkills
snap := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
```

Never let tests reach the real `~/.agents/` or `~/.cache/` — always
mock via the swap helpers.

### The organization layer is already isolated — don't re-arrange it

`testutil.IsolatedHome()` covers `~`, but the organization layer sits at
a fixed absolute path (`/etc/agent-marketplace/config.yaml`) that no
`HOME` override reaches. So under `go test` an unset override resolves
to a nonexistent path instead of the real file
(`config.organizationConfigPath`), and a test that loads effective
config sees an absent organization layer.

Do not reproduce this per package. It used to be a per-package chore,
which meant one omission was enough: a test reaching the real file
inherited that machine's registries and cloned them over the network,
presenting as a ten-minute hang rather than a failure.

To exercise an organization config, name one explicitly:

```go
config.SetTestOrganizationConfigPath(t, orgPath)       // package-level, restored on cleanup
pipeline.LoadEffective(ctx, pipeline.ConfigLoadStrict, // or per call; ConfigLoadOnline only when the
    config.LoadEffectiveOptions{                       // test permits transient acquisition
        OrganizationPath: orgPath,
    }, nil)
```

E2E is separate: it drives the built binary rather than a test binary,
so the sandbox points it at its own `etc/` through
`AGENT_MARKETPLACE_V3_ORG_CONFIG` under the `e2e` build tag.

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

`loadCatalogsForTUI` calls `catalog.LoadRegistries` in cached mode, which
skips `cloneOrPull` for warm cache entries and reports `RefreshAfterLoad`
(`internal/catalog/loader.go`); the TUI then starts a background
`refreshSnapshot`. If startup is slow, check that the warm-cache detection
there still matches discovery for your new registry type.

## What Not To Do

- Don't re-derive selection logic in the TUI or a new subcommand. Go
  through `pipeline.Resolve`.
- Don't edit YAML by re-marshaling `Config` structs. You'll drop
  comments. Use the layer editor (`pipeline.EditLayerAtPath`) or the
  `internal/config/editor_helpers.go` node helpers.
- Don't add a CLI command that duplicates a TUI-only flow. Either
  factor the shared logic into `internal/` first, or keep it
  TUI-only.
- Don't introduce blocking git fetches on the render path. Catalog
  refreshes run as `tea.Cmd` and emit `refreshLoadedMsg`.
- Don't silently succeed when a config write targets the wrong list.
  Cover each scope with a test that verifies the intended list received
  the write (see `TestReadSnapshotProjectToggleWithPerEntrySkills`).
