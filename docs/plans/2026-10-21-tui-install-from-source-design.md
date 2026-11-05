# TUI Install From Source

## Goal

Let a TUI user install resources from a Git, GitLab/GitHub web, or local
directory source that is not yet configured, without leaving the TUI. Today the
TUI only toggles resources from configured catalogs (Space) and installs
Marketplace discovery rows; `install --from` exists only on the CLI.

The flow follows the Marketplace admin registry editor: describe the source,
preview what discovery finds, decide, then confirm. Nothing is written before
confirmation, and the install refuses if the source moved since the preview.

## Non-goals

- Registering a source without selecting resources from it.
- Adding Marketplace endpoints or installing Marketplace locators (existing
  discovery rows already cover Marketplace installs).
- Tag or `--all` installs, `--as` renames, and harness targeting in this modal.
- Changing the Space toggle modal. Configured catalogs keep using it.

## User flow

Entry: `i` in the resource list, and an "Install from source…" palette action.
Both are refused with the existing guard toast when the consumer configuration
is unsupported or invalid, or another operation is active.

The modal has three steps with a step indicator (`① Source  ② Pick  ③ Review`,
shortened to `2/3` on compact terminals). Esc cancels from any step; Backspace
on an empty field or in Pick returns to the previous step.

### 1. Source

```
╭─ Install from source ───────────────── ① Source  ② Pick  ③ Review ─╮
│ URL   https://github.com/acme/agent-skills/tree/main/skills█        │
│ Ref   (default branch)                                              │
│ Path  (registry root)                                               │
│                                                                     │
│ Web URLs fill ref and path automatically. Nothing is written yet.   │
╰─ tab next field · enter preview · esc cancel ───────────────────────╯
```

Three Huh text inputs. Enter resolves web URLs, then validates the allowlist
and registry path before any network access. Rejections render inline under
the offending field.

### 2. Preview and pick

```
╭─ Install from source ───────────────── ✓ Source  ② Pick  ③ Review ─╮
│ 14 resources at 3f2a9c1e04b7 on main · 9 skills · 3 commands · 2 mcp │
│ ⚠ 1 malformed resource skipped (d: details)                         │
│ /  filter                                                          │
│ [x] skill    pdf-tools          Extract and fill PDF forms           │
│ [ ] skill    git-helpers        conflicts: installed from acme/core  │
│ [ ] command  review             ✓ already installed (P)              │
│ [ ] collection  docs-pack (4)   Writing and docs bundle              │
│ …                                                       2 selected   │
╰─ space toggle · a all · / filter · enter review · ⌫ back · esc ─────╯
```

- The header shows the resolved commit, the ref (or "default branch"), and
  counts per kind.
- Rows are grouped by kind and show name, description, and one status badge:
  already installed (with scope letters), conflicts with an installed resource
  from another source, or malformed (dimmed, not selectable).
- `a` toggles every selectable row that matches the current filter.
- `d` expands catalog diagnostics. A source that matches a configured registry
  shows a banner saying so; picking still works.
- An empty source shows "Nothing discoverable under this source. Check that it
  points at the directory containing skills/, commands/ or a plugin manifest."
  and Enter is disabled.
- A source that expands into several sub-bundles lists their paths; Enter on
  one fills Path and previews again.
- A fetch failure shows the sanitized error and "⌫ edit source"; the typed
  fields are kept.

### 3. Review

Choose the scope with `g` (user), `p` (project), or `l` (local). Ineligible
scopes are greyed out with the reason, for example a repo-relative local path
cannot be written to user scope. The step shows the exact layer file that will
be written, the selected resources, a collision warning when a pick conflicts
with an installed resource, and, for a branch ref: "`main` is a branch; it can
move before install. Use a commit SHA in Ref to pin." `y` or Enter confirms.

### Result

The modal closes and the install runs as the usual guarded mutation with
progress. Success reloads the authoritative snapshot, shows the new rows as
pending sync, and toasts "Installed 2 from acme/agent-skills in this repo".
There is no separate result step.

## Architecture

### TUI (`internal/tui/install_source.go`)

- `installSourceModal` state: step (`source`, `pick`, `review`), the three
  inputs, the `SourcePreview`, a picked set keyed by `kind/name`, filter text,
  cursor, and scope.
- New `paneInstallSource` in pane routing, so q, Esc, and Ctrl+C route before
  any form submits (spec "Explicit modal intent").
- `keyMap` gains `InstallSource` (`i`) and its help text; footer, `?` overlay,
  and palette render from it.
- Badges are derived from `m.rows`: same kind and name means "already
  installed"; same kind and name with a different `RegistryURL` means
  "conflicts". No extra I/O.
- All remote text passes through `sanitizeTerminalData`.

### Read-only preview (`pipeline.PreviewSource`)

Extract the body of `runListFrom` (`cmd/agent-marketplace/list.go`) into:

```go
func PreviewSource(ctx context.Context, ws Workspace, url, ref, path string) (SourcePreview, error)

type SourcePreview struct {
	URL, Ref, Path string // resolved
	Commit         string // what Ref resolved to; empty for local directories
	Catalog        catalog.LoadedCatalog
	SubBundles     []string // non-empty when the source needs a Path
	Warnings       []error  // overlap and requires-invalid warnings
}
```

Order: `ResolveFromURL`, `ReadIntent` + `ValidateRegistryURLs` +
`ValidateRegistryPath`, default-branch resolution, then
`catalog.LoadRegistriesWithContext(LoadModeRefresh)` over a synthetic v4
config. `list --from` calls the same function, so CLI and TUI cannot disagree.

The TUI runs the preview as a `tea.Cmd` under a new `requestInstallPreview`
request kind; a superseded response is dropped, and Esc cancels its context.

### Install (`pipeline.Install`)

Confirm builds `pipeline.InstallOptions{FromURL, FromRef, FromPath, Locators,
Scope, ExpectedCommit}` and runs it through the same guarded path as Marketplace
discovery installs (`beginOperation(operationMutation)`,
`DependencyReconcile = m.dependencyLifecycle.InstallReconcile`, operation
progress, authoritative reload, toast). The registry entry and its selections
publish in one layer transaction with the layer lock, as `install --from` does.

Two additions to `InstallOptions`, both used only by the `--from` path:

- `Locators []config.ResourceLocator`: kind-qualified picks, so mixed kinds
  with shared names are unambiguous. `Args` stays for the CLI; both feed
  `planNamedInstalls`.
- `ExpectedCommit string`: when set, `installFromAdHoc` compares the commit it
  fetched with it and refuses before any write if they differ. The TUI returns
  to Pick with "`main` moved from 3f2a9c1 to 81b0d2e; preview again", keeping
  picks that still exist. Config records the ref the user typed; the lock
  records the commit.

## Error handling

Every row below leaves configuration, acquisition lock, and harness bytes
unchanged.

| Case | Behavior |
|---|---|
| Unsupported/invalid consumer config, or busy | Entry refused with the existing guard toast |
| Blocked URL or invalid path | Inline field error; no network access |
| Fetch failure, unknown ref, unreachable host | Failure state in Pick; fields kept |
| Empty source | Hint shown; confirm disabled |
| Several sub-bundles | Paths listed; choosing one fills Path and re-previews |
| Malformed resources | Dimmed, unselectable; count in header, details on `d` |
| Name conflict with installed resource | Badge; Review warns it may collide |
| User scope with repo-relative path | `g` disabled with reason |
| Ref moved since preview | Refused by `ExpectedCommit`; back to Pick |
| Install fails mid-transaction | Nothing published; error toast and diagnostic |
| Compact terminal | Single column, no descriptions; resize keeps picks and cursor |

## Open questions verified before implementation

1. `catalog.LoadedCatalog` does not carry a commit. The first task confirms the
   Git cache can report the resolved commit for the synthetic entry, and how
   `installFromAdHoc` can read the commit it fetched.
2. `installFromAdHoc` refuses a source that resolves to more than one catalog.
   The first task confirms `PreviewSource` can detect that case from
   `LoadedCatalog.SubBundles` and return the sub-bundle paths.
3. Collections are picked by name today (`planNamedInstalls` matches `Args`
   against discovered collections). The first task confirms whether
   `config.ResourceLocator` can carry a collection pick; if not, collection
   picks travel in `Args` alongside kind-qualified `Locators`.

If any assumption fails, the design returns for review before code is
written.

## Alternatives considered

- **Register the source, toggle later.** Simpler, but writes configuration
  before the user has seen the source.
- **Install everything from the source.** Matches `install --from` without
  names, but gives no choice and surprises users with large registries.
- **Scope on the Source step.** Rejected: deciding what before where matches the
  admin editor and lets the preview show installed state at every scope.
- **Pin config to the previewed commit.** Rejected: users who type a branch
  expect to track it. `ExpectedCommit` gives the same safety without changing
  what config records.

## Specification and documentation

OpenSpec change `tui-install-from-source`:

- `tui-interactions`, ADDED "Install from an unconfigured source": preview
  writes nothing; pick and install at the chosen scope (config, lock, harness);
  blocked URL refused offline; moved ref refuses the stale install; cancel at
  any step preserves bytes; malformed and empty sources are not installable.
- `resource-installation`, MODIFIED: `ExpectedCommit` and kind-qualified
  `--from` installs.
- `ad-hoc-registry-browsing` is unchanged; `list --from` keeps its behavior.

Update `site/docs/reference/tui.md` and `docs/tui-product-contract.md` for the
`i` binding and flow.

## Testing

- `internal/pipeline`: `PreviewSource` (allowlist rejected before fetch,
  default branch, sub-bundles, malformed); `Install` with mismatched
  `ExpectedCommit` leaves bytes unchanged; mixed-kind `Locators`. Each test
  carries a `// spec:` annotation.
- `internal/tui`: step transitions, stale preview generation dropped, guard
  refusal, scope eligibility, compact resize.
- TUI QA journey `tools/tui-qa/tests/install-from-source.spec.ts` with a new
  `adHocSource` fixture option (a second, unconfigured local Git registry with
  two skills, one command, and one malformed skill):
  1. From the settled state, press `i`, enter the URL, and assert the preview
     header shows counts and commit.
  2. Assert the malformed skill is not selectable; pick one skill and the
     command, press Enter, choose `p`, and assert Review lists both picks and
     `.agents/config.yaml`.
  3. Push a change to the source, confirm, and assert the moved-ref message
     with config bytes unchanged.
  4. Confirm again; assert both rows in the main list, the repo config and lock
     record the source and selections, and after `s` the harness files exist.
  5. Esc from each step leaves bytes unchanged.
  Register the journey in `tools/tui-qa/README.md`.
- Gates: `make spec-check`, `make validate`, `make tui-qa`, and
  `go run ./tools/specaudit gate -base origin/main -head HEAD`.
