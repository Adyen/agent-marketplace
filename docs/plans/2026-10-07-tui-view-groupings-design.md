# Selectable TUI Groupings and Section Bulk Toggle

**Date:** 2026-10-07
**Status:** Approved design; implementation not started
**Worktree branch:** `toggle-view`
**Design base:** `690ee9d05f6460fd7c99a9b7830a2ac3ac55b122`

## 1. Decisions and objective

v4.1.0 (`tui-single-status-view`) replaced the Scope, Registry, and Status tabs with one status-grouped list. Users with roughly 300 resources across several kinds and registry subpaths report that they can no longer browse by resource kind or by source, and can no longer act on "everything from `frontend`" efficiently.

- Keep the status-grouped list as the default. Add a **View settings** modal that selects a primary grouping (Status, Source, Kind) and an optional secondary grouping.
- Group Source by **configured source entry**: repository identity plus ref and registry subpath, labelled as before v4.1 (`repo / subpath`, or `(local)`).
- Make **Space on a section header** open a bulk toggle modal that applies the existing per-resource toggle to every eligible row in the section, in one staged transaction.
- Persist view settings in a per-user state file under `$XDG_STATE_HOME`, never in authored configuration.

This document records the approved design only. It does not authorize code, spec, or test changes; those follow through the OpenSpec change `tui-view-groupings`.

### User feedback addressed

1. "Tabs for categories like Skills, MCPs, and Subagents" and "sorting the items by type and then by name": the Kind grouping.
2. "I liked the grouping per source … I could simply disable anything coming from frontend": the Source grouping at registry-subpath level plus the section bulk toggle.

### Rejected alternatives

| Alternative | Reason not chosen |
|---|---|
| Restore the tab bar | Reverses the archived `tui-single-status-view` decision and costs a content line; the grouping setting provides the same views |
| Kind/source filter rail (instead of grouping) | Does not provide the browse-by-source overview; may be revisited after groupings ship |
| Source-level bulk writes (`all: false` / clearing `include`) | Chosen against: per-resource writes reuse the existing toggle path and scope semantics. Consequence accepted: resources a source adds later are not covered by a past bulk action |
| Group by folders inside a source tree | Users meant configured registry subpaths, not resource directory nesting |
| New `T` key for bulk toggle | Space already means "toggle the thing under the cursor"; folding remains on Enter, `za`, `h`, and `l` |
| Persist settings in `.agents/config.yaml` | Display preferences do not belong in committed or layered config; would touch the v4 schema, layering, and the released-client gate |

### Out of scope

- Source-level selection writes, filter rails, and tabs.
- Grouping by resource directory nesting or collections.
- Any change to config or acquisition lock formats, resolution, selection policy, ownership, or materialization.

## 2. Grouping model

### Axes

| Setting | Values | Default |
|---|---|---|
| Group by | Status, Source, Kind | Status |
| Then by | None, Kind, Source (excluding the primary axis) | None |

Status is never offered as a secondary axis: the row status glyph already carries it, and the Needs-attention section (below) surfaces actionable states.

- **Status** keeps the current order: Required, Enabled, Pending sync, Stale, Broken, Available, Malformed.
- **Source** groups by configured source identity `(normalized URL, ref, registry path)`. The label is the pre-v4.1 `registryDisplayLabel` form: registry display name, ` / <full normalized subpath>` when a subpath exists, `(local)` without a recorded registry. Labels are sanitized for terminal output as section titles are today. Sections sort by label, then identity, so equal labels from different refs stay distinct and deterministic.
- **Kind** uses the order Skills, Commands, Subagents, Hooks, MCP servers, Collections, Rules, then unknown kinds alphabetically (the pre-v4.1 `orderedResourceKinds`).

Example, Group by Source, then by Kind:

```
Needs attention (3)
▼ awesome-ai / frontend (42)
  ▼ Skills (30)
      P ✓ skill  awesome-ai/react-patterns
  ▶ MCP servers (12)
▼ awesome-ai / backend (61)
```

### Needs attention

When the primary axis is not Status, rows classified Pending sync, Stale, Broken, or Malformed move into a pinned top section `Needs attention`. Rows are moved, not duplicated: selection anchoring (`rowKey`) assumes a row appears once. Required and Available rows remain in their groups and keep their status glyphs. Inside Needs attention, rows are sorted by status order then name.

### Ordering, search, and folds

- Leaf rows are alphabetical when unfiltered and search-ranked when filtered, exactly as `sortLeafRows` does today. Empty sections are omitted, and sections without matches are hidden during search.
- The search index adds the resource kind and the source label, so `mcp` or `frontend` narrows the list under any grouping.
- Fold paths carry the axis: `status:<bucket>`, `source:<identity>`, `source:<identity>:kind:<kind>`, `kind:<kind>`, `kind:<kind>:source:<identity>`, `attention`. Fold state is kept per grouping combination for the session, so switching grouping and back restores folds.
- On a grouping change, the selection stays on the same resource if it is still visible; otherwise on the section that now contains it; otherwise on the first item.

### View settings modal

- Opened with `v` in the resource list (currently unbound) or the palette action "View settings…". Help lists both.
- A form with the two radio fields. Changes preview live in the list behind the modal; Enter keeps them, Esc restores the previous settings.
- No tab bar or permanent indicator line; the list keeps its v4.1 height. The footer may show the active grouping as a low-priority segment dropped first under width pressure.

### Persistence

- File: `$XDG_STATE_HOME/agent-marketplace/tui.json`, falling back to `~/.local/state/agent-marketplace/tui.json`. Sandboxed command tests already isolate XDG roots.
- Content: `{"version": 1, "view": {"groupBy": "source", "thenBy": "kind"}}`. Fold state is not persisted.
- Written atomically (temp file plus rename) only when the user confirms a changed setting. Read once at startup.
- A missing file means defaults. An unreadable, malformed, or unknown-version file, or unknown axis values, fall back to defaults with one footer warning; startup never fails because of this file, and the TUI does not overwrite it until the user saves new settings.
- Never reads or writes authored config or acquisition locks.

## 3. Section bulk toggle

### Entry point

- Space on a section header opens the bulk toggle modal. Space on a resource row is unchanged.
- Folding a header remains on Enter, `za`, `h`, and `l`. Help changes Space to "toggle resource / section" and keeps Enter as "details / fold".
- The palette adds "Toggle all in section…", following the same eligibility as the key.
- Space remains inert while a sync is running, as it is today.

### Target set

1. All resource rows under the header, including rows in folded subsections.
2. If a search filter is applied, only matching rows; the modal states the filter and the match count.
3. Rows excluded with a reason, reusing the single-row eligibility rules (`applyRowSpaceEligibility` and `TestToggleEligibilityParityMatrix`): Required, Malformed, Unavailable/Broken, cached discovery rows that cannot be installed from the list, and rows already in the chosen target state.

A header with no eligible rows still opens the modal and lists the skip reasons, so Space has one meaning on headers.

### Modal

```
┌ Toggle all · awesome-ai / frontend ─────────┐
│ 42 resources (filter "react": 7 match)      │
│   project  on 30 · off 12                   │
│   local    on  0 · off 42                   │
│ Skipped: 2 required, 1 malformed            │
│                                             │
│ Scope: [g]lobal [p]roject [l]ocal [e]xclude │
│ Then:  [+] enable all   [-] disable all     │
│ Resources added later are not covered.      │
└─────────────────────────────────────────────┘
```

- The user picks a scope with the existing `g`/`p`/`l`/`e` keys, then an explicit direction. Mixed sections have no meaningful flip, so the bulk action never flips row by row.
- A confirmation step shows the number of resources to change and the target config layer before any write. Esc at any step closes without writing.
- Scope availability follows the single-row modal (for example, project scope requires a workspace).

### Write path

- Build the per-row `toggleAction`s the single toggle would produce for the chosen scope and direction, and apply them as **one staged layer-editor mutation** of the chosen config layer: every `include`/`exclude` key is written or none is.
- Run through the existing mutation serialization and authoritative reload, producing one pending-sync outcome and one reload instead of one per row.
- Failure keeps the config bytes unchanged and reports through the existing footer error path, matching `v4_toggle_failure_test.go`.
- A stale completion (state changed since the modal opened) is rejected by the existing request generation check; the modal re-evaluates eligibility on submit, as the palette toggle does today.

### Telemetry

One action span per submitted bulk toggle with the grouping axes, targeted and skipped counts, scope, and direction. No resource names, labels, or paths.

## 4. Specs and verification

OpenSpec change `tui-view-groupings`:

| Capability | Delta |
|---|---|
| `tui-navigation` | Replace "Single status-grouped resource list" with status-by-default selectable groupings; Needs attention; fold paths per grouping; search over kind and source; former tab shortcuts stay inactive |
| `tui-interactions` | View settings modal; Space on a section header opens the bulk toggle; bulk target set, eligibility, explicit direction, confirmation, atomic write |
| `tui-rendering` | Source and Kind section labels and nesting within supported terminal dimensions |
| `tui-feedback` | Help and palette entries; bulk toggle telemetry; state-file warning |
| `tui-startup` | Persisted view settings applied at startup; corrupt or unknown file falls back to defaults |

Tests, each annotated with `// spec: <capability>/<scenario>`:

- Tree builders for every grouping combination, Needs attention membership and ordering, deterministic labels for equal-named sources, fold path isolation, and selection continuity across grouping changes.
- Search matches on kind and source label under every grouping.
- State file: round trip, atomic write, missing/corrupt/unknown version/unknown value fallbacks, no write without a confirmed change, no config or lock access.
- Bulk toggle: eligibility parity with the single-row matrix, filter-limited targets, folded descendants included, all-or-nothing staged write, single reload, stale completion rejection, Esc writes nothing.
- Former tab keys remain inactive under every grouping.
- TUI QA journey: switch to Source → Kind, fold a subpath section, bulk-disable it at project scope, confirm, and verify the pending sync state; restart and verify the grouping persisted.

Gates before handoff: focused `internal/tui` tests, `make validate`, `make spec-check`, `make tui-qa`.
