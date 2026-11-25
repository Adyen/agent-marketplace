# Design

## Context

See `proposal.md` for motivation. The current workspace already filters by source and applies search after the selected row scope. Sidebar groups and the source list occupy a flex column with a fixed footer, and Settings already owns source removal confirmation and mutation.

## Goals / Non-Goals

**Goals:**

- Keep facets local to Resources and compose them with the existing source and search filters.
- Present the selected source's projected metadata and safe contextual actions without exposing raw configuration.
- Reuse existing Settings removal and shared clipboard behavior.
- Preserve the current GUI tokens, grouping, counters, source-list scrolling, and footer layout.

**Non-Goals:**

- Changing grouping preferences, local API contracts, persisted configuration, session counters, or Marketplace discovery behavior.
- Reworking the wider GUI visual design or adding a refresh action.
- Adding kind facets unsupported by the desktop row projection.

## Decisions

### Derive facets from the accepted source rows

Keep source identity and association matching unchanged. Filter `sourceRows` by the selected kind and status, then pass that subset to the existing row ranking/search path. This makes the result an intersection while keeping discovery requests and session-wide counts independent. Unknown resource kinds remain visible when no kind is selected. Kind labels come from the current grouping titles without changing grouping order.

Status facets are local predicates: Needs attention selects stale, broken, or malformed rows; Required by org selects rows whose accepted projection marks them required. Each is a single toggle, independent from the kind and source selections. A new All resources action clears these facets and source selection while leaving the active query untouched; the existing Resources and group navigation retain their current behavior.

### Keep controls compact and independently scrollable

Use the existing sidebar rows and color/surface tokens for status and kind controls. Put the facet and group controls in a bounded, scrollable region, with kind options in a compact grid. Keep the Sources heading and source list outside that region; the source list remains its own `min-height: 0` scroller, and the footer remains non-shrinking. This prevents the added controls from collapsing the source list at short window heights.

### Project source context from the selected descriptor and accepted rows

Render a source header only for a selected source in Resources. Use the selected `SourceDescription` for kind, projected location, ref, and subpath; use the existing source label for its heading. Count only stale, broken, or malformed rows already attributable through the current source filter. The problem action selects Needs attention without clearing the kind, source, or query.

Keep the source actions in a native disclosure so they remain keyboard accessible. Opening is offered only when a strict parser accepts an absolute HTTP(S) URL without credentials, whitespace/control characters, query, or fragment; otherwise no browser action is rendered. Copy only the projected `location`, using a shared helper that preserves the Wails clipboard preference and browser fallback already used for diagnostics. Escape closes the disclosure and returns focus to its summary. Key the header by source identity so its disclosure and feedback state cannot carry across sources.

### Route source removal through Settings

The header offers removal only when the workspace configuration is editable and the selected source's identity is present in `userSources`, and no operation is busy. A removal request opens Settings → Sources and carries that source ID and a sequence number. Settings waits until it is open and loaded, rechecks that the same ID is still an editable user source and removable, then selects the existing confirmation state. Confirmation and cancellation continue through the existing Settings controls and controller operation; the header never mutates configuration directly.

### Preserve the existing visual hierarchy

Use the existing GUI font, spacing, surfaces, borders, and focus treatments. Keep the toolbar and resource grouping unchanged. The source header is a restrained metadata row above the list, with its source name as the heading and location metadata allowed to wrap rather than force horizontal scrolling. Facet chips share the current source-filter area and wrap within the main column.

## Risks / Trade-offs

- [The added sidebar controls could reduce room for sources at short heights] → Bound and isolate facet/group scrolling, keep the Sources list separately scrollable with a nonzero minimum, and retain the fixed footer; cover the 1024×360 source-tooltip journey.
- [A source descriptor may disappear or change while Settings opens] → Revalidate the source ID against the loaded editable user source list before opening confirmation.
- [A copied location may be redacted] → Copy only the already projected descriptor value; never read or log raw configuration.
- [External URLs may contain unsafe or private syntax] → Keep the allowlist narrow and test rejected URL forms without launching a browser.

## Migration Plan

No migration is required. The change does not alter configuration, local API schemas, or persisted preferences.
