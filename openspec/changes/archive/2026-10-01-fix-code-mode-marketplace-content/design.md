## Context

See proposal.md for motivation. The Marketplace loader already has exact coordinate and resource revision information, a metadata cache, and canonical artifact verification. Canonical support files decode separately from legacy asset files.

## Goals / Non-Goals

Keep the fixes inside the existing loader and public skill bindings. Preserve cache-backed offline access, exact catalog revision selection, lazy server startup, and the two-tool MCP contract. Do not add eager catalog artifact fetches, a new cache schema, freshness configuration, or hosted-service changes.

## Decisions

- Fetch exact revision metadata on every online content verification. Reuse the existing client identity validation and persist its result. A timeout or unavailable response refuses access rather than using a previously allowed revision. A freshness TTL or retry-only-on-404 would leave already-cached content and policy stale.
- Keep the existing offline path, with validated metadata and verified artifacts required. Offline access cannot discover later revocation and makes no freshness claim.
- List canonical Support and legacy Assets together by decoded relative path. Reject overlaps even when bytes match because path ownership would otherwise depend on arbitrary precedence. Preserve sorted 512-path listing and the 4 MiB read bound. Licenses outside these file sets are not newly exposed.
- Check the artifact against the expected archive and content digests, not only its own manifest. A self-consistent different artifact is not the selected revision.
- Use local HTTP fixtures and isolated caches to prove stale metadata, support-file reads, denied fallback, digest substitution refusal, and offline reuse through the script consumer. Existing unrelated snapshots and fixtures remain unchanged.

## Risks / Trade-offs

- Every online verification requires metadata I/O, including existing gate/load repetitions. Retain the simple correctness boundary and measure the affected load path; removing repeated verification is separate work.
- New archive metadata can refer to an unavailable artifact. Fail with the existing coded missing-content error; do not change hosted policy or hide catalog entries through eager downloads.
- New online availability checks can deny content already cached locally. This is intentional fail-closed behavior; explicitly offline access remains cache-only.

## Migration Plan

No configuration or cache migration is required. Publish the loader fix with the updated specification and documentation; an online access refreshes the exact metadata entry. Rollback restores the previous loader without deleting cached artifacts.
