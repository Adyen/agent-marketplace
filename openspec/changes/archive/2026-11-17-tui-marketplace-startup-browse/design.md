# Design

## Context

See proposal.md for motivation. `app.go` already calls empty-query discovery at the config and final snapshot stages; `discovery.go` caches browse rows and restores them when search clears. The internal Marketplace discovery client fetches one page with a maximum of 50 items. Complete startup browsing must therefore extend pagination and lifecycle continuity rather than introduce a second independent browser or block snapshot loading.

## Goals / Non-Goals

**Goals:** progressively complete the configured Marketplace inventory with responsive cold and warm startup and stable search/selection.

**Non-Goals:** no periodic polling, automatic installation, resource-content prefetch, new endpoint registration, public API/schema change, CLI search change, or arbitrary catalog-size truncation.

## Decisions

- Extend the existing validated discovery path to follow the API's pagination, retaining page-size limits, endpoint concurrency bounds, and cancellation rather than making one unbounded response or raising a result cap.
- Retain endpoint-bound browse inventory separately from query results. Apply the active local filter to arriving rows; search cancellation must not permanently lose the incomplete browse inventory. Use generation/sequence checks for endpoint changes and reloads.
- Coalesce equivalent startup stage requests. Reuse valid cached pages first and fetch missing or stale pages in the background under existing freshness policy. A fresh first page alone is not evidence the whole catalog is loaded.
- Preserve existing identity, allowed-endpoint, verification-pending, install guards, and installed-coordinate deduplication. Discovery only fetches metadata and writes its existing disposable cache, not resource content or configuration.
- Use client/model tests for page completion, empty pages, late responses, endpoint errors, and cache behavior. Add a terminal journey with more than 50 resources and explicit delayed-page signals, navigation before release, search/clear, and consumer-state preservation.

## Risks / Trade-offs

- [Large catalogs increase total startup traffic] → bounded page requests and concurrency, cache reuse, cancellation on quit, no polling; measure fresh-process cold/warm startup, first interaction, total requests, and memory with local endpoints.
- [Search hides resources while pages arrive] → retain browse inventory independently and test overlap, completion, and clearing explicitly.
- [Later-page failure looks complete] → retain successful rows and report incompleteness; do not cache an incomplete catalog as complete.
- [Shared discovery implementation changes] → implement after the kind-search change and reuse its validated pagination support without changing CLI search semantics.
