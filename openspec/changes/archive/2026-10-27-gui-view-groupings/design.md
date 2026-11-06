## Context

The TUI groups resources through `internal/tui/tree.go` and persists the choice in `tui.json`. The GUI is a webview with its own state, so it ports the grouping rules and stores the choice in webview `localStorage`. Source identity comes from the snapshot, because the webview cannot derive it from registry metadata itself.

## Decisions

- **Grouping logic is pure.** `gui/src/grouping.ts` builds an ordered section tree from search matches, so tests cover every axis combination without mounting components.
- **Source identity is server-derived.** Each row carries `source: {id, label}`. The id mirrors the TUI `sourceIdentity` (normalized URL, ref, and subpath, or `local`). The label mirrors `session.SourceLabel` after terminal sanitization.
- **Folds are keyed by combination.** Browsing folds are stored per `${groupBy}/${thenBy}` so returning to an earlier grouping restores its folds. Search folds stay query-local and reset when grouping changes.
- **Persistence is local.** The choice lives under `am-gui-view` in `localStorage`. Malformed, invalid, or unavailable storage falls back to Status and None.
- **Controls.** The View menu is a popover with two radio groups. Choices apply immediately, Escape closes it and returns focus to the View button, an outside click closes it without moving focus, and the `v` key opens it under the same rules as other single-letter keys.
- **Out of scope.** The TUI "Toggle all in section" bulk action is not ported.

## Risks

- Selection and viewport anchoring rely on visible entries. Grouping changes rebuild the entry list, so selection must be resolved from the previous visible entries before the rebuild completes.
