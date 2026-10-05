## Context

The TUI uses Status tree construction alongside two alternative groupings and per-tab fold, filter, and cursor state. Label rendering already separates status symbols, resource labels, scope badges, harness badges, and name-match highlighting. Shared configuration and synchronization logic do not need to change.

## Goals / Non-Goals

Goals:
- Make the current status tree the only list while preserving its ordering, folds, search, and selection anchoring.
- Show scope letters before kind and registry-qualified names at all supported widths.
- Keep provisional row continuity and all mutation guards.

Non-goals:
- Release changes, new dependencies, alternative views, registry subpath labels, registry resolution, or search matching changes.
- Configuration, acquisition lock, ownership, and materialization changes.

## Decisions

Use `buildStatusTree` directly, keep one fold-state map on the model, and remove tab state, key bindings, palette actions, telemetry naming, and styles. Preserve `status:*` section identifiers and existing sort semantics. A hidden tab switcher would contradict the requested single view.

Keep `originBadge` as the scope authority, padded to three cells before the kind. Remove list word-chip fallback. Build `resourceLabel` as kind plus registry display name and trailing resource name; sanitize all label data before rendering. The trailing name allows name-match indexes to shift past the prefix. Keep selection styling and the harness override suffix unchanged.

Use the recorded registry display name, otherwise the existing URL basename helper, otherwise `(local)`. Omit registry subpaths even when two configured paths produce the same visible prefix; row identity and the detail pane still distinguish them. Longer qualified rows use the existing bounded wrapping, not truncation that hides names.

Remove the tab line from layout accounting so the list gains one line. Retire only the tab attribute from detail browse spans; other action attributes and telemetry policy stay untouched. Keep native modal Tab navigation.

Update observable boundary tests before production edits. Add self-contained tests that also compile on the previous version so missing behavior can be demonstrated by assertions. Adapt existing selection, provisional, rendering, palette, and integration tests to the single view without weakening them. Replace obsolete view-switch steps in direct real-terminal journeys with equivalent inspection, search, folds, and scrolling. Retain actual before/after terminal captures at identical dimensions and fixture state.

Measure status recomputation using the retained status subbenchmarks. Measure list rendering on the same status-tree fixture before and after using a dedicated benchmark. Keep initialization outside timing, report allocations, and do not describe more expensive qualified labels as a speed improvement.

## Risks / Trade-offs

- Registry-qualified labels wrap more often. Preserve complete names and exercise minimum dimensions and viewport movement.
- Removing alternative trees changes default fixture shape. Adapt affected assertions to Status semantics without removing selection, toggle, or parity coverage.
- Sanitized registry text can shift highlight offsets. Test hostile text and Unicode prefixes with explicit terminal color profiles.
- Different registry subpaths may have identical visible labels. This is an intentional compact-label choice; preserve full identity and detail metadata.
