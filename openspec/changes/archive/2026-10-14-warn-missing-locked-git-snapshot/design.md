## Context

Exact Git inspection deliberately does not acquire missing snapshots. A pulled acquisition lock can therefore refer to a valid remote commit while the consumer still has only an older cached snapshot. The shared lock view also labels untrusted snapshots with `lock.snapshot-unavailable`, so its diagnostic code is insufficient to identify a recoverable absence.

## Decisions

- Change only TUI presentation of snapshot inspection issues whose underlying cause is `cache.ErrExactGitSnapshotUnavailable`. Preserve wrapping and the original lock context; do not alter the shared pipeline or operation-failure diagnostics.
- Preserve the pipeline's hard catalog-failure and unavailable-selection evidence so warning severity cannot establish verified drift or authoritative success.
- Prioritize the existing sync shortcut for this recovery state, without introducing a second mutation path.
- Prove the newer-lock/stale-cache case, pinned recovery, untrusted-cache distinction, unsuccessful sync, and actual terminal interaction. Keep the existing disk-I/O benchmark as the healthy-path performance control.

## Alternatives

Automatically fetching during inspection would change its read-only contract. Downgrading the shared diagnostic code would hide corrupt snapshots and weaken command checks. Advancing the lock or falling back to the mutable ref would break reproducibility.

## Compatibility

No changes to configuration, acquisition-lock formats, ownership state, selection precedence, materialization, or CLI exit-status policy.

## Verification evidence

Focused command regressions passed for both v2 and v4 with plain, locked, and TUI synchronization. Scoped `make validate` passed the complete short-mode TUI and pipeline packages, vet, lint, and build. The real-terminal gate passed all 22 journeys and four process tests; the strengthened recovery journey passed again independently. Five new scenarios are trace-bound.

Five-sample, two-second disk-I/O benchmarks of warm locked Git startup with 200 resources initially measured 355.1 ms on the base and 430.6 ms on the candidate. A contemporaneous unchanged-base control measured 452.1 ms, demonstrating substantial host timing variation; its comparison with the candidate was not significant (`benchstat`, p=0.841). Both retained approximately 40.2 MB and 273.6k allocations per operation. These runs include filesystem work but no remote network requests and do not establish a speedup.

Footer controls retained identical bytes and allocation counts (startup: 3001 B/46 allocations; operation: 3514 B/53 allocations; error: 1504 B/28 allocations). Their median times were 14.04 to 13.41, 16.86 to 16.04, and 7.542 to 7.457 microseconds respectively. The existing benchmarks remain as regression controls; five samples are insufficient for a 95% confidence interval.

OCR delegation review covered all three automatically selected files and all ten supplementary test, documentation, and spec files. Review checked TUI entry-point reachability, missing versus untrusted causes, unverified drift, failed operations, unchanged CLI failures, pin preservation, and inspection's absence of writes. No actionable findings remained. Runtime Git proof files were excluded from the intended diff.
