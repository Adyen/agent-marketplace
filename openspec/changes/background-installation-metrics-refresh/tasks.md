## 1. Regression proof and baseline

- [ ] 1.1 Add scenario-bound blocked-telemetry search, scheduling/freshness/lifecycle and timing configuration tests; run focused tests to observe the intended failures before implementation.
- [ ] 1.2 Capture matched base search benchmarks with local 25-ms telemetry and warm lookup, retaining five two-second allocation-reporting samples outside the repository.

## 2. Background installation refresh

- [ ] 2.1 Implement optional operator timing defaults, overrides and overflow-safe validation independently of reindex timing; prove loader and direct-config behavior with config tests.
- [ ] 2.2 Separate refresh, atomic immutable lookup and serial scheduling with coalesced hints/backoff; prove failure retention, hard expiry, identity changes, horizon recovery and no overlapping attempts.
- [ ] 2.3 Wire the worker into API startup/cancellation/joining and search lookup; prove non-blocking startup/search and shutdown cancellation with lifecycle tests.
- [ ] 2.4 Add bounded per-attempt refresh metrics and safe failure logs; prove operations observations and privacy without per-search attempt reporting.

## 3. Integration and publication claims

- [ ] 3.1 Adapt existing reader/search fixtures without weakening parser, identity, ranking, pagination or credential-boundary coverage; run the affected API/config packages and race tests.
- [ ] 3.2 Capture candidate benchmark samples with unchanged inputs and commands, retain benchstat comparison and before/after blocked-backend evidence.
- [ ] 3.3 Reconcile operator and HTTP reference documentation with defaults, background behavior, last-good retention and expiry; verify all relevant published claims against implementation.
- [ ] 3.4 Run native validation, strict OpenSpec/specaudit checks and relevant Marketplace process E2E; retain commands, outcomes and scope/criterion coverage.
- [ ] 3.5 Run `openspec archive background-installation-metrics-refresh --yes` in the same pull request and verify archived source content and synced main capability.
