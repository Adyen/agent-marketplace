## Context

The approved interaction and preference design is recorded in `docs/plans/2026-10-07-tui-view-groupings-design.md`. The existing model rebuilds a flattened Status tree from filtered rows and keeps folds in memory. Per-row mutations already use a staged layer editor and the dependency lifecycle's serialized mutation boundary.

## Goals / Non-Goals

Implement the approved selectable groupings, XDG preferences, and confirmed per-resource section bulk toggle. Preserve the default status list, single-resource actions, config formats, policy, ownership, and materialization. Do not restore tabs, add filter rails, persist folds, write source-wide selections, or group by resource directory.

## Decisions

- Use a `groupingAxis` string type with Status, Source, Kind, and None values and `viewSettings` with GroupBy/ThenBy JSON fields. Defaults normalize to Status/None. One `buildGroupedTree(rows, settings, foldState)` builds every combination; the old Status builder remains a compatibility seam for existing tests/callers.
- Keep raw source identity separate from sanitized labels. Reuse normalized URL/ref/path identity helpers and the old repository/subpath display format. Use unambiguous encoded identity in fold paths, not display labels. Keep fold maps keyed by complete grouping combination.
- Move attention rows before grouping the remaining rows. Do not duplicate resources or change the existing status classifier. Add kind and source label to the existing cached search metadata, not to the fuzzy resource-name channel.
- Rebuild the view with an explicit selected row anchor when settings preview changes, falling back to the deepest visible containing header. Modal state retains original settings for cancellation. View settings can preview while async snapshots arrive without claiming a mutation; submissions preserve existing operation guards.
- Store preferences in a TUI-local versioned JSON reader/writer, separate from config loading. Use owner-private temporary files and atomic rename; load failure becomes a footer warning. Tests must isolate XDG/HOME, not read developer state. Do not add a permanent footer grouping indicator.
- Collect bulk members from an expanded tree of current filtered rows, not the visible flattened list. Capture section identity at open and re-evaluate its current members on submit. Use explicit direction; for exclude scopes, enabling the resource clears exclusion and disabling sets it.
- Share the existing per-resource layer-editing callback with the bulk path. Validate the entire action set before publication, require one destination layer, then apply all callbacks in one staged editor transaction inside one dependency lifecycle mutation. A mixed set whose existing exclusion semantics resolve to different layers must refuse publication rather than partially commit. Marketplace direct pins and collection selectors must retain the existing pin semantics; use the layer editor's Marketplace source/include/remove support rather than invoking independently committing per-pin operations. Cached discovery requiring acquisition is not a bulk config-only toggle.
- One correlated completion initiates one authoritative reload. Eligibility, workspace scope, request generations, collisions, and config validity are checked before the transaction; errors preserve bytes and use existing feedback. No optimistic row-by-row state changes or reloads.
- Reuse existing forms and bounded modal framing, with field/stage navigation when all choices do not fit at minimum size. Help/palette dispatch by action identity, not synthetic keys. Emit one privacy-safe bulk action span only on submission.

## Risks / Trade-offs

Space on a section no longer folds, but Enter, za, h, and l remain. Source label collisions require stable raw identities. Group membership can change while a modal is open, so confirmation must describe current eligibility and never commit a stale target set. A staged callback error after an earlier edit must not publish any edit. Bulk actions affect only the resources present now, not future additions.

## Verification

Table-driven Go tests cover all delta scenarios with spec bindings, preserving existing eligibility parity and generation guards. One direct terminal journey switches to Source → Kind, folds a source subsection, bulk-disables matching resources at project scope, observes pending sync, and restarts to prove preferences persist. Baseline and candidate search/view benchmarks retain realistic fixtures. Native/spec and terminal gates run on the clean candidate, followed by independent acceptance and adversarial review.
