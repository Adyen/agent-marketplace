# Design

## Context

The sidebar (`gui/src/Sidebar.vue`) lists configured sources flat, and every row plus the ＋ button emit `openSources`, which `WorkspaceView.vue` turns into `openSettingsWindow('sources')`. Settings → Sources (`SettingsWindow.vue`) renders user and organization sources with `SourceList.vue`. `SourceDescription` carries `kind`, `location`, `ref`, `path`, `name`, and an opaque `id`.

## Goals / Non-Goals

Goals: grouped subpath sources, deep links from the sidebar into Settings → Sources, and source search. Non-goals: configuration format changes, new source kinds, local API changes.

## Decisions

- **One pure grouping and filtering module.** A shared `gui/src/source-groups.ts` turns a source list into entries (a single source, or a group keyed by layer, kind, and location with two or more sources) and filters by query. Sidebar and Settings both use it, so the two views cannot disagree.
- **Group key is layer + kind + location.** Sources from different layers stay apart because Settings already lists the layers separately. The ref is shown on the child rather than splitting the group.
- **Folds are session-only and per view.** Each view keeps its own set of folded group keys. Separate state is what makes "reveal unfolds the folded Settings group" observable and keeps the sidebar layout stable when Settings folds.
- **Reveal through a request token.** `WorkspaceView` passes Settings a reveal request (source id plus a counter) or an add-form focus request alongside the tab. Settings applies it after configuration loads: clears the search, unfolds the group, scrolls the card into view, focuses it, and marks it highlighted until the window closes or another reveal happens.
- **The ＋ keeps the accessible name "Sources".** Existing tests and the focus-return path use that name; only its destination focus changes.

## Risks / Trade-offs

- Configuration loads asynchronously when Settings opens, so the reveal waits for it; a source removed in between is ignored and the Sources tab opens normally.
