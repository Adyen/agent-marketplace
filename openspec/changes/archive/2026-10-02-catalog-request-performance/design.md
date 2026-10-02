## Context

The public stack runs authority, availability, then existing mux metrics. Authority reads and decodes the managed change record, and database-mode availability reads it again before decoding the policy snapshot. Catalog reads currently rank wide rows with all assessment and revocation joins before limiting a page. Both stores decode findings and collection JSON before the handler compares ETags. The handler's DTO validation occurs after the matching-304 return; preserve that distinction rather than inventing stronger conditional validation accidentally.

## Goals / Non-Goals

Goals are representative full-handler measurements on both stores, additive bounded timing, request-local state reuse, guarded conditional reads, measured allocation reductions, and page-before-decoration selection with unchanged public correctness.

No general projection cache, cross-request managed snapshot, schema migration, pool retuning, configuration knob, deployment or resource change, unrelated client/frontend/publication/archive change, or production performance claim.

## Decisions

### Serial execution with frozen measurements

Retain one parent-authored benchmark fixture that exercises all six resource kinds, direct collection relations, managed configuration, and first1/100/500, continuation and304 cases through PublicHandler. Run identical fixtures five times before and after each final optimization under the same local backend conditions, retaining ns/op, B/op and allocs/op. Store CPU/allocation profiles and pool-wait evidence outside the repository. Numeric investigation targets are stretch goals, not production SLOs or brittle correctness assertions.

### One request-local managed change

Keep the existing authority guard for startup, readiness, admin and file mode. Factor shared authority validation around an already-read ManagedChange and its read error, then carry the accepted database-mode change privately in request context. Availability consumes it rather than fetching again. Direct availability middleware tests retain a fallback read when the authority wrapper is intentionally absent. Decode into a fresh availability view per request; no global cache or shared map. Preserve exact authority-before-availability failure boundaries and health exemptions.

### Additive metrics

Keep existing request/duration/active series at their current mux boundary. Add a public-stack duration/request observation outside authority and availability, plus fixed phase labels for authority, availability, catalog read, catalog decoration and catalog response work. Resolve bounded registered route patterns through the mux for early failures, never by labeling the raw path. Ensure route-pattern propagation does not alter mux redirects or method rejection. No pprof route; private Go benchmark profiles suffice. Pool waits are measured at the local fixture boundary without retuning the pool.

### Authoritative conditional reads

Do not return304 from catalog version alone. A conditional-capable store must read the version and any integrity-bearing page data in one existing store snapshot and retain findings and collection JSON rejection equivalent to the current scan. The reduced path may avoid wide assessment/revocation payloads, relation visibility hydration, availability decisions and DTO serialization only after those checks. A store without that capability takes the existing full-read path. No cross-request cache or weakened corruption handling is an acceptable fallback.

### Page keys before wide decoration

Within one store snapshot, select eligible current occurrence/assessment candidates, preserve descending digest winner selection, apply the stateless coordinate cursor, stable ordering and limit-plus-one to narrow keys. Only then join the selected page with wide payloads and revocations and hydrate collection relations. Eligibility must still require the actual resource assessment row, not merely a current-assessment pointer. PostgreSQL and SQLite use backend-appropriate forms of the same selection semantics. Preserve the existing lookahead behavior that does not decode the extra row.

### Profile-led allocation work

Use frozen baseline profiles to identify the next repeated decode or canonicalization cost. Retain equivalent validation and sanitized output; avoid an unmeasured rewrite. Each retained change traces to an observed cost and gets a focused regression where skipped work can be asserted without a timing threshold.

## Risks / Trade-offs

- A cheap version check would suppress unchanged-version corruption detection. Narrow integrity reads retain some304 work; correctness takes precedence over the stretch target.
- Moving wide joins past limiting can change eligibility or winners if a pointer references a missing assessment. Test duplicate and legacy candidates explicitly.
- Context propagation and middleware response wrappers can change metrics or headers. Test complete actual PublicHandler behavior and early503s.
- Managed updates are not globally atomic with catalog publication; retain the existing coherent per-request managed snapshot and store page snapshot rather than claiming a new cross-domain transaction.
- PostgreSQL measurements require a disposable isolated fixture. Missing infrastructure is a recovery prerequisite, not a passing skip.
- Concurrent archive repair shares broad package globs but not catalog/authority/availability/metrics logic. Coordinate new overlap before editing shared interfaces.
