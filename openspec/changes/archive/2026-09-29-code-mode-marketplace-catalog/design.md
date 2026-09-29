## Context

See proposal.md. `SnapshotBuilder` currently reads `CachedDiscovery` in both cached and refresh modes. That cache holds a capped discovery query, not a complete Marketplace catalog. The Marketplace API already defines a cursor-based catalog with a version and ETag. The script engine passes Go structs directly to goja.

## Goals / Non-Goals

**Goals:** Keep the synchronous startup a cache-only read. Refresh each configured Marketplace independently in the background, and preserve artifact verification at execution time.

**Non-Goals:** Do not replace TUI discovery, add a fallback for endpoints without the catalog API, wait on cold start, or repair failed MCP server starts.

## Decisions

- Scan `GET /api/v1/catalog` to `complete: true`, validating the configured and well-known instance IDs and stable catalog version across pages. Bound pages, resources, response bytes, and elapsed time. A scan that stops early is an error. Discovery paging cannot provide a complete, stable catalog, so it is not reused.
- Persist complete per-endpoint catalog records, including instance ID, version, ETag, fetch time, and resources. Use the Marketplace cache's existing safe write/lock approach, and reject corrupt or unsafe cache records. A failed refresh does not overwrite the last verified complete record; the published snapshot reports stale for a failed endpoint. A first-page 304 can reuse only a matching complete record. Never send conditional requests for later pages.
- Keep snapshot construction in two modes: cached read at startup, network scan on refresh. Return one outcome per configured endpoint, including failures or misses, so no missing endpoint appears healthy. Convert catalog resource coordinates into existing code-mode refs and origins; leave verified artifact acquisition and gating in the loader.
- Convert Go MCP tool metadata and results to plain script values via JSON marshaling and unmarshaling. This preserves their wire field names and avoids exposing raw schema bytes as JavaScript arrays.

## Risks / Trade-offs

- A large catalog increases scan work and snapshot memory. Explicit aggregate bounds and per-endpoint timeouts turn oversized results into stale warnings, never truncated success.
- Background refresh can fail while the previous complete cache is usable. Retain that cache for future startup and report the current refresh failure as stale rather than silently treating old data as new.
- Changing Marketplace state during paging can produce mismatched versions. Abort and retry on the next refresh; never combine inconsistent pages.

## Migration Plan

The new complete cache has a separate schema and does not import single-page discovery entries. A first run can be stale until background refresh succeeds. Existing installations keep their config and verified artifact cache. Reverting the code leaves the prior discovery cache intact.
