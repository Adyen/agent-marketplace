# Design

## Context

See proposal.md for motivation. `internal/tui/state.go` already indexes kind identifiers and labels for loaded rows. The missing seam is hosted discovery: `loadMarketplaceDiscovery` sends the query as free text without `DiscoveryOptions.Kind`, and `DiscoverUpdates` retrieves at most 50 results per endpoint. Existing TUI request generations, 250ms debounce, cache-first delivery, endpoint identity checks, and installed-coordinate deduplication must remain authoritative.

## Goals / Non-Goals

**Goals:** reach hosted kind-only matches through existing search and support all pages of those matches.

**Non-Goals:** no slash query language, exclusive kind-filter syntax, public API or CLI search change, startup inventory feature, or installation/configuration policy change.

## Decisions

- Derive recognized kind queries from existing kind parsing and display labels, not a separate MCP-only name heuristic. Keep ordinary text matches additive; do not replace name/metadata search with an exclusive kind filter.
- Use the existing API kind filter to discover hosted kind-only matches, merge with ordinary text results by endpoint/resource coordinate, and retrieve continuation pages for kind matches. Reuse existing client validation, endpoint concurrency limits, and cancellation. Do not widen the server's free-text semantics.
- Keep local matching immediate and remote work debounced. Preserve generation isolation across paginated updates and query changes. Add shared pagination support only as needed by this result, leaving full empty-query startup loading to its own change.
- Prove with model/client tests plus a mixed-kind Marketplace terminal journey where opaque names and descriptions cannot accidentally satisfy text matching. Cover more than 50 MCP resources, multiple endpoints, failed endpoints, clearing, and late results.

## Risks / Trade-offs

- [Broader kind queries increase requests] → page requests remain bounded and cancellable; no background polling or arbitrary total-result cap.
- [Union results alter selection] → retain existing ranking and deterministic coordinate deduplication; test name matches against kind-only matches.
- [Per-keystroke overhead] → retain and extend search benchmarks; collect baseline and candidate measurements as repository standards require.
