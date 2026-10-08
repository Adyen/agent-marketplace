## Context

`rebuildFlatItems` replaces the fold map with an empty map whenever a query is applied. Existing controls still update browsing folds, so their changes are invisible until search clears. `selectionKeyForRebuild` also treats cursor zero as having no anchor, which can move selection away from the first folded header during refresh.

## Goals / Non-Goals

Keep category folds usable under the applied query and preserve browsing folds. Query changes reveal matches, while unchanged-query refreshes retain deliberate folds and header selection.

Do not change ranking, match counts, key bindings, configuration, resource actions, remote APIs, or persisted state.

## Decisions

Keep the existing browsing fold map and add a query-local map plus the query that owns it. `currentFoldState` supplies the effective map: discard query folds on clear, initialize an expanded map on query change, otherwise reuse it. All fold controls already use this seam except expand-all, which will clear the effective map. `rebuildFlatItems` uses the effective map without a second override.

Preserve a real section selection at cursor zero during active-query rebuilds. Keep the existing unfiltered initial-selection behavior unchanged.

Rejected alternatives: honoring browsing folds directly hides matches on search start; auto-expanding only while the input is focused would still mutate browsing folds and reset folds when reopening an unchanged query.

## Risks / Trade-offs

Fold state must reset on every query change, including paste, backspace, and direct recomputation during tests. Detecting the query transition at the shared fold-map seam covers those paths without duplicating lifecycle logic. Tests exercise real key messages and accepted discovery/snapshot consumers rather than only inspecting maps. The existing search view benchmark measures the added query comparison.
