## 1. Measurement and request state

- [x] 1.1 Retain the representative full-public-handler benchmark fixture and first1/100/500, continuation and304 cases; verify all cases produce the expected status and page cardinality on both stores before optimization, then capture five baseline samples and private CPU/allocation profiles.
- [x] 1.2 Add failing PublicHandler tests for one managed configuration read, in-flight coherence, next-request freshness and concurrent isolation; replay on base, implement request-local reuse, and verify focused authority/availability tests pass.
- [x] 1.3 Add failing tests for additive full-stack and fixed-phase metrics including early failures and label privacy; implement instrumentation and verify existing metric series remain unchanged.

## 2. Conditional reads and page selection

- [x] 2.1 Specify the reduced conditional-store interface and integrity query from retained measurements; verify the design preserves findings and collection JSON rejection before dispatching implementation.
- [x] 2.2 Add failing matching-validator skipped-work and unchanged-version corruption tests on both stores, including malformed headers, weak/list/wildcard GET/HEAD and continuation controls; implement the guarded fast path and verify focused tests pass.
- [x] 2.3 Add duplicate/legacy eligibility, missing assessment/occurrence, winner, lookahead and publication regressions; move both stores to narrow ordered winning page selection before wide decoration and verify backend parity.
- [x] 2.4 Preserve collection hydration, moving scans, source sanitization, current managed/revocation invalidation and Marketplace isolation; verify complete response outcomes and read-only state for success and failure cases.

## 3. Measured reduction and integration

- [x] 3.1 Select allocation or repeated canonicalization reductions from frozen private profiles, retaining equivalent validation; verify focused regressions and identical before/after benchmark evidence for each retained change.
- [x] 3.2 Bind all ten public scenarios with test annotations and verify every changed path serves the optimization and safety requirements; run affected consumer regressions and strict specification tracing.
- [x] 3.3 Run identical final five-sample benchmarks on both stores, compare time/bytes/allocations, capture pool waits and private profiles, and document fixture boundaries and limitations in review evidence.

## 4. Delivery

- [ ] 4.1 Reconcile relevant operations metric and catalog claims with implementation; verify existing metric semantics and avoid unsupported production latency claims.
- [ ] 4.2 Run native validation, strict OpenSpec validation and touched domain gates on the clean candidate; verify complete passing output and no skipped required backend evidence.
- [ ] 4.3 Run `openspec archive catalog-request-performance --yes` in the same pull request and verify strict validation and scenario bindings after the main-spec sync.
