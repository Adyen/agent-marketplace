- [x] Reproduce legacy unmarked hard-link failures in sync and check.
- [x] Restrict captured I/O to declared relocated aggregates.
- [x] Reproduce and refuse Git administrative roots and aliases.
- [x] Share the existing relative-value predicate.
- [x] Validate regressions, affected packages and domain gates; adversarial review.
- [x] Archive this clarification as the final commit.

## Final Verification

Scoped consumer regressions, CI lint/build, the combined E2E gate (four required
witnesses and 114 command requirements), and all 86 terminal journeys passed.
At main integration, production and module files matched that validated
candidate. The inherited startup
cleanup tests and strict spec checks also pass. All 26 new PR scenarios are bound.

The final review makes the private captured-I/O mode explicit rather than using
a nil sentinel. Both existing I/O paths remain unchanged; focused Claude, prune
and JSON writeback regressions plus CI lint/build pass after that clarification.
