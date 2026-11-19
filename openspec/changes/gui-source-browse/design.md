## Context

The sidebar row currently reveals its configuration entry. The desktop local API uses one discovery result stream for the active query; Marketplace configuration entries do not carry the same matching key used for Git and directory rows. The TUI's progressive Marketplace implementation provides reusable paging and cache behavior but is not on the reviewed GUI branch yet.

## Goals / Non-Goals

- Keep browsing and managing sources distinct. Carry the complete, progressive Marketplace browse behavior into the desktop GUI.
- Do not redesign grouping, Add source, resource mutations, the TUI or the Marketplace server. Do not introduce consumer configuration or cache format forks.

## Decisions

Mockup (not built): Resources shows team-tools and platform-core. Activate team-tools and the list narrows in place, with a named removable filter beside the workspace heading. Manage source opens and highlights that source in Settings. An empty result stays scoped so it cannot be mistaken for the full catalog.

For a Marketplace source, received rows are usable while later pages are pending. Typing a query switches the visible result set, not the background inventory; clearing the query restores the still-filtered browse list. A failed later page leaves usable rows visible with incomplete discovery in Diagnostics.

Use the existing sidebar/settings reveal and the local API source identity seam, preserving server-side sanitization. Keep browse state separate from query state at the desktop adapter; share Marketplace paging/cache and reuse ordered GUI events and cancellation where practical. The selected filter is view state, not authored configuration or an input to mutation authorization.

## Risks / Trade-offs

A source name is not an identity, and late packets must not cross endpoint configuration or request generations. Do not let new page requests widen endpoint permissions, write managed files, or make partial cache entries look complete. Do not duplicate the shared pager to work around a missing dependency.
