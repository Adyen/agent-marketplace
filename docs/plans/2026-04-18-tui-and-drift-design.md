# TUI and Drift Detection Design

**Date:** 2026-04-18
**Branch:** `tui`
**Status:** Implemented

## Summary

Add an interactive terminal UI to `agent-marketplace` for browsing, enabling,
and disabling resources (skills, commands, subagents, hooks, MCP servers). Along the way, introduce a shared **drift** primitive
that answers the question "is this repo in sync with its config?" — a signal
previously computed ad-hoc inside `status`/`sync` and invisible to users of
`list`.

The TUI becomes the default entrypoint when invoked on a TTY with no
subcommand. All existing CLI subcommands remain supported for scripting, CI,
and non-interactive environments.

## Goals

- Browsing available skills across registries should feel as fast as `fzf`.
- Enabling or disabling a skill should take one keystroke.
- It should always be obvious, from both TUI and CLI, whether the repo's
  on-disk skills match its committed config.
- Zero new business logic in the TUI layer; it consumes the same `internal/*`
  packages the CLI already uses.
- No change to config file formats, paths, or semantics.

## Non-Goals

- Editing registry entries from the TUI (`$EDITOR` is the right tool).
- Authoring or editing `SKILL.md` files from the TUI.
- Exposing `gc`, `install --tag`, or `update --bump` in the TUI — rare ops
  stay on the CLI and are linked to from the help overlay.
- File-watching `.agents/config.yaml` — an `r`-to-reload keybinding is
  enough and avoids a second code path.
- Detecting upstream-of-cache drift (that's what `update --check` is for).

## User Experience

### Layout

Two-pane Miller columns with a header and a persistent footer:

```
+-- agent-marketplace -------------- repo: ~/code/my-repo  branch: main --+
| Skills (37)            / search                                         |
+-------------------------------+-----------------------------------------+
|  [x] vm-lifecycle    infra    | vm-lifecycle                            |
|  [x] k8s-deploy      k8s      | Registry: example-infra-skills (main)     |
|  [*] tls-cert-rotate sec      | Tags: infra, vm                         |
|  [ ] firewall-rules  sec ex   | Status: enabled (synced)                |
|  [R] audit-logging   sec req  | Source: ~/.cache/agent-marketplace/...  |
| >[ ] my-local-skill  loc      |                                         |
|                               | # Description (from SKILL.md)           |
|                               | Provision and tear down example VMs...  |
|                               |                                         |
|                               | <space> toggle | <enter> full view      |
+-------------------------------+-----------------------------------------+
| ~ 2 out of sync  ?  help  /  search  s  sync  u  update  q  quit        |
+-------------------------------------------------------------------------+
```

### Row glyphs

Each glyph reflects the drift state of one skill:

| Glyph   | Meaning                                                  |
|---------|----------------------------------------------------------|
| `[ ]`   | available in a registry, not enabled                     |
| `[x]`   | enabled and synced                                       |
| `[*]`   | enabled in config, missing on disk (pending sync)        |
| `[~]`   | symlink exists but points at wrong cache/ref             |
| `[-]`   | removed from config, symlink still on disk (pending gc)  |
| `[!]`   | symlink present but target unreadable (cache wiped)      |
| `[R]`   | required by global config, cannot be toggled off         |
| `ex`    | row suffix — excluded via repo or local config           |

### Keybindings

- `j` / `k` / arrow keys — move cursor; `g` / `G` — top / bottom.
- `space` — toggle enable/disable. Writes config immediately, leaves sync
  for later (see "Hybrid commit" below).
- `/` — fuzzy search across name, tags, and registry; `esc` clears.
- `enter` — full-screen SKILL.md viewer (markdown rendered by Glamour);
  `q` / `esc` returns.
- `s` — apply pending changes (`sync`); footer spinner during git I/O.
- `u` — refresh registry caches (`update`); footer spinner.
- `r` — reload from disk (picks up external edits to config files).
- `e` — expand errors into a scrollable pane.
- `?` — help overlay.
- `q` — quit; warns with a confirm overlay if pending changes are unsynced.

### Hybrid commit semantics

A toggle writes `.agents/config.yaml` (or `.agents/config.local.yaml`)
immediately — config changes are cheap and safe to persist. Reconciliation
of symlinks is deferred until the user presses `s`. This matches the speed
users expect from a TUI while keeping the slow git operations (clone, pull,
checkout) explicit and interruptible.

Implication: a toggled-on skill renders as `[*]` (enabled in config, not on
disk yet). The footer shows `~ N out of sync` so the user always knows what
`s` will do.

## Architecture

### Library choice

`github.com/charmbracelet/bubbletea` + `bubbles` + `lipgloss` + `glamour`.
These are the de-facto Go TUI stack, pure Go (no cgo), MIT-licensed, and
their Elm-style `Model` / `Update` / `View` cleanly mirrors our hybrid
commit state machine.

`rivo/tview` was considered and rejected: its widget/event model forces
imperative state mutation that clashes with our "config mutation as state"
flow.

### Package layout

New files only; no existing file is renamed or deleted.

```
cmd/agent-marketplace/
  main.go       # rootCmd.RunE launches TUI when no args + isatty
  tui.go        # newTUICmd() for explicit invocation, launches app

internal/tui/
  app.go        # tea.Model: top-level Update/View, routes to panes
  list.go       # left pane: filtered skill list + row rendering
  detail.go     # right pane: metadata + markdown viewport
  footer.go     # pending count, spinner, shortcuts
  help.go       # "?" overlay
  keys.go       # centralized key.Binding definitions
  state.go      # domain state: skills, pending mutations, filters
  actions.go    # tea.Cmd wrappers around internal/config, /sync, etc.
  theme.go      # Lipgloss styles

internal/drift/
  drift.go      # Compute, Report, Item, Reason, Scope
  drift_test.go # missing/stale/orphaned/broken matrices
```

### Separation of concerns

The TUI never calls `cobra` or writes to `os.Stdout` directly. It talks
only to the existing `internal/config`, `internal/resolver`,
`internal/cache`, `internal/sync`, `internal/skill`, and the new
`internal/drift` packages. Concretely:

- `space` -> `config.AddSkillToConfig` / `config.AddExcludeToConfig` —
  the same calls `install`/`remove` use today.
- `s` -> `sync.Run`.
- `u` -> the logic currently in `update.go`.

This keeps CLI commands as the one true implementation; the TUI is a
second presenter over the same core.

### Entrypoint rule

In `main.go`:

```go
if len(os.Args) == 1 && term.IsTerminal(os.Stdin.Fd()) &&
    os.Getenv("AGENT_MARKETPLACE_NO_TUI") == "" {
    return runTUI(cmd.OutOrStdout())
}
```

A hidden `--no-tui` flag and the `AGENT_MARKETPLACE_NO_TUI=1` env var
provide escape hatches for CI and scripted wrappers even when stdin is a
TTY. An explicit `agent-marketplace tui` subcommand exists for scripts
that want to launch the TUI unconditionally.

## State Machine

```go
type Model struct {
    repoRoot string
    merged   *config.Config
    catalogs map[string]loadedCatalog
    resolved []resolver.ResolvedSkill
    drift    drift.Report
    rows     []row
    cursor   int
    filter   string
    syncing  bool
    lastSync time.Time
    errs     []error
    pane     pane          // list | detail | help | confirm | errors
    viewport viewport.Model
}
```

### Lifecycle

1. **Boot** — load global + repo + local configs, merge, load catalogs
   (cold-start may trigger clone/pull behind a spinner), resolve, compute
   drift. Emit `snapshotLoadedMsg`.
2. **Toggle (`space`)** — compute the new desired state, write the
   appropriate YAML file via a `configWriteCmd`, re-run `resolver.Resolve`
   and `drift.Compute` in-memory (no disk re-read), update the row glyph,
   bump the footer counter.
3. **Sync (`s`)** — no-op if drift is clean. Otherwise spawn
   `syncCmd` around `sync.Run` behind a spinner. On success re-walk disk,
   recompute drift, assert `InSync()`.
4. **Update (`u`)** — wrap the `update.go` logic, re-run discovery and
   drift afterwards so new skills appear in the list.
5. **Quit (`q`)** — if drift has pending outbound changes, open a confirm
   overlay: `y` sync+quit, `n` quit anyway, `esc` cancel.

### Error handling

Every `tea.Cmd` returns either a success message or `errorMsg{op, err}`.
Errors never crash the TUI — they accumulate in `Model.errs`, footer shows
`! 3 errors (e)`, `e` opens a scrollable error pane. Required-skill
toggle attempts surface as a 2-second toast, not a modal.

## Drift as a Shared Primitive

### Why

`status.go` already computes most of what we need, but in an ad-hoc
shape: missing symlinks leak into the `TARGET` column as `error:` strings,
and the "symlink points at wrong ref" case isn't detected at all. Users
have no single-line answer to "do I need to run sync?".

Making drift a proper package gives us a shared vocabulary testable in
isolation and exposable to any frontend.

### API

```go
package drift

type Reason int

const (
    MissingLink Reason = iota  // in config, no symlink
    WrongTarget                // symlink exists, points at unexpected cache/ref
    OrphanedLink               // symlink exists, not in config
    DeadLink                   // symlink target is missing on disk
    StaleCache                 // advisory: cache mtime > threshold
)

type Scope int

const (
    Repo Scope = iota
    Global
)

type Item struct {
    Skill    string
    Scope    Scope
    Expected string  // empty for OrphanedLink, DeadLink
    Actual   string  // empty for MissingLink
    Reason   Reason
}

type Report struct {
    Missing  []Item
    Stale    []Item  // WrongTarget
    Orphaned []Item
    Broken   []Item  // DeadLink
    CacheOld []Item  // advisory
}

func (r Report) InSync() bool
func (r Report) PendingCount() int

type DiskPaths struct {
    RepoSkillsDir   string
    GlobalSkillsDir string
}

func Compute(
    resolved []resolver.ResolvedSkill,
    paths DiskPaths,
    cacheAges map[string]time.Time,
) Report
```

`Compute` performs only `os.ReadDir`, `os.Lstat`, and `os.Readlink`. No
network. Cheap enough to run on every TUI boot and every `r` reload.

### Consumers

- **TUI** — stores `Report` in `Model.drift`; drives glyphs and the
  footer.
- **`status`** — renders drift sections using `Report` instead of ad-hoc
  helpers. The existing `findOrphanedSymlinks`, `symlinkTargetDisplay`
  error handling, and `describeCacheFreshness` are folded into
  `drift.Compute`.
- **`sync`** — uses `drift.Compute` to decide reconciliation actions;
  clearer error messages become possible ("removed stale symlink for
  `vm-lifecycle`: was `main@abc123`, expected `main@def456`").
- **`list`** — appends a one-line footer when not in sync; silent when
  clean.
- **`sync --check`** — new flag that runs `drift.Compute`, prints the
  dirty sections, exits non-zero on drift. Designed for pre-commit hooks
  and CI.

### Out of scope for drift

- Upstream-of-cache drift (remote has newer commits). That belongs in
  `update --check` and is reported separately as an advisory, never as
  "out of sync".
- Content tampering inside the cache. The cache directory is read-only
  by contract.

## CLI Impact

| Command | Change |
|---|---|
| `agent-marketplace` (no args, TTY) | Launches TUI instead of printing help. Escape hatch: `--no-tui` or `AGENT_MARKETPLACE_NO_TUI=1`. |
| `agent-marketplace tui` | New explicit subcommand, always launches TUI. |
| `agent-marketplace sync` | Unchanged surface. Internals now use `drift.Compute`. Clearer error messages. |
| `agent-marketplace sync --check` | New flag. Prints drift diff, exits 0 if clean, 1 if drifted. |
| `agent-marketplace status` | First line becomes `sync: OK` or `sync: N out of sync (...)`. Existing tables render from drift. **Exit code stays 0** (use `sync --check` for programmatic gating). |
| `agent-marketplace list` | Appends one-line drift footer when not in sync. Silent otherwise. Exit code unchanged. |
| `install` / `remove` / `update` / `gc` | Surface unchanged. |

No config format changes. No Makefile or CI pipeline changes beyond the
new dependency in `go.mod`.

## Rollout Plan

Each step is a standalone PR; each keeps the tree shippable.

1. **Extract `internal/drift`.** Refactor `status.go` and `sync.go` to
   consume it. Tests cover all four drift categories. No user-visible
   behavior change.
2. **Add `sync --check` and the `list` drift footer.** First user-visible
   drift surfacing. Update the README "Commands" block.
3. **Scaffold `internal/tui/`.** Add Bubble Tea and friends to `go.mod`.
   Ship the list pane, detail pane, and `/`-search. Wire to the new `tui`
   subcommand. Toggling not yet implemented.
4. **Wire toggling and sync actions.** `space` toggles with hybrid
   commit semantics; `s` applies; `u` refreshes caches; `r` reloads;
   `e`/`?` overlays.
5. **Make TUI the default for TTY + no-args.** Add `--no-tui` and
   `AGENT_MARKETPLACE_NO_TUI`. Release as `v1.1.0` with a prominent
   migration note.

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Scripts relying on `agent-marketplace` with no args printing help will break when TUI becomes default. | Isatty check short-circuits piped/redirected invocations. `--no-tui` flag and env var. Prominent release note. Conservative opt-in period: ship as subcommand first, flip default in a later PR. |
| Bubble Tea adds binary size and a dependency surface. | Charm libraries are pure Go, MIT, widely deployed. Static binary constraint preserved. |
| TUI obscures long-running git operations. | Explicit spinner in footer for `s`/`u`; list remains scrollable (read-only) during sync; errors surface in `e` pane. |
| Drift computation misses a category we haven't considered. | `Reason` enum is extensible; tests are matrix-style so adding categories is cheap; we explicitly scope upstream drift and content tampering out. |
| Hybrid commit confuses users ("why isn't my skill synced?"). | Footer always shows `~ N out of sync`; row glyph `[*]` is distinct from `[x]`; `q` triggers confirm overlay when drifted. |

## Open Questions

None — all blockers resolved during brainstorming. See the
brainstorming transcript for the decision log.
