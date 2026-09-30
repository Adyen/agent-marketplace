## Context

The unlocked-endpoint cache walker rejects non-JSON entries at the revision level and empty directories at intermediate levels. The cache writer creates intermediate directories and `.tmp-*` files before renaming completed metadata into place. Shared inspection already classifies catalog malformed resources as warnings, but status adds its own malformed error rows.

## Goals / Non-Goals

Keep live artifacts reachable across interrupted writes, reject malformed completed data with bounded memory, and reuse one inspection classification across consumers. Do not expand cache pruning or alter acquisition authority.

## Decisions

- Skip only writer-owned temporary files and empty intermediate directories; do not skip non-empty unexpected paths or malformed completed JSON. Reuse the cache's file-containment validation for completed revision reads and cap their size.
- Status uses the inspection's issue collection for both warning and error rendering. Avoid a status-only malformed conversion and source-path heuristic. Sync check prints warning categories from inspection rather than rebuilding malformed classification.
- For lease-time tests, block collection on the exact-cache lease, edit applicable inputs, then release and assert retained artifacts or a generation conflict. Compare dry-run candidates with actual collection on an unchanged fixture.

## Risks / Trade-offs

Metadata outside an active endpoint remains outside this collector. Ignoring incomplete writer-owned entries avoids a persistent GC failure without treating completed corrupt metadata as safe. Bounded reads add a local size limit and fail closed above it.
