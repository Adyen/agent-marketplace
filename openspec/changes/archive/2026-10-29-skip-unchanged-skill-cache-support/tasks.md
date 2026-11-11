## Implementation

- [x] Red/green unit and real-`Run` regressions: an unchanged repeat creates no support stage and preserves cache entry identity and mtime and the installed link.
- [x] Drift matrix covering source bytes, mode and membership, and cache tampering, missing and extra entries and link replacement; it passes before and after the change.
- [x] Reuse the existing exact virtual and actual tree digests; fall back to full preparation on any mismatch or error.
- [x] Paired benchmarks and warm-`sync` workflow measurements with an unaffected no-support control.
- [x] Full materialization suite, validation, specs, E2E and adversarial review.
- [x] Archive as final commit.
