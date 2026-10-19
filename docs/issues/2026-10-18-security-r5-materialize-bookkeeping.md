# Security R5: SDK Materialization Bookkeeping (R35)

## Scope

This independent slice starts at main `de87921b` while R28 PR #743 is queued.
PR #743 merges before integration gates; this slice is rebased onto
`ced6f887`. The SDK implementation and filesystem boundary dependencies are
unchanged across the rebase. R28's JSON-array change does not affect the
unchanged non-fragment path used by these ordinary-file benchmarks.
Frozen R35/A112/F79 (`01a0a06b-ae6f-765c-abbf-efe4993ebffa`) concerns
unchanged/skipped artifact bookkeeping in public SDK `Materialize`.
The frozen ledger/source index remain unchanged from `2f55f85f`.

No D1-D6 decision, new ceiling, truncation or filesystem policy is introduced.
This is not a change to CLI selection, translation, JSONC patching, ownership
or source-revision identity.

## Confirmed Cost And Change

Both staging and publication scanned the complete ordered `report.Skipped`
list for every artifact. For an all-unchanged generation, each loop performs
`N*(N+1)/2` path comparisons, despite knowing which files need writing
during the earlier byte-comparison pass.

Planning now compacts its already-local artifact slice into the write list.
Only that list reaches staging/publication; no new map, helper, dependency
or allocation is added. The separately populated all-artifact path set is
unchanged, so skipped files remain protected from stale-file pruning.
The staged-identity map's capacity now follows the actual write count too.

Compaction preserves relative order and does not mutate the caller's
`Result`. Reports keep their complete ordered skipped/written paths.
Byte comparison, source/root/file identity checks, mode handling, marker
merge preparation, partial-result rules, pruning and cleanup are unchanged.
In particular there is no all-skipped early return: empty-write-list calls
still pass root admission and the established staging lifecycle.

This shared SDK serves conversion callers, CLI materialization and derived
link caches. Tests and measurements use the actual public SDK, not a
reimplementation of the membership loop.

## Measurements And Limits

Go 1.27.1, darwin/arm64, Apple M4 Max; identical benchmark source/fixtures,
sequential `-benchtime 2s -count 5 -benchmem` runs without concurrent local
validation. Inputs are ordinary SDK file artifacts on isolated temporary
filesystems, repeatedly materialized after initial seeding.

Timed work includes actual SDK disk reads, path and identity admission,
staging, publication when needed and cleanup. Initial materialization,
dirty-fixture resets, output-preservation assertions, compilation and network
are excluded. These are warm local-filesystem measurements of the public SDK,
not full CLI sync or cold-cache timings.

Observed medians:

| Fixture | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| 1 skipped file | 391,409 | 396,835 | 18,006 | 18,006 | 190 | 190 |
| 64 skipped files | 6,241,835 | 6,583,218 | 550,856 | 541,321 | 4,885 | 4,882 |
| 1,024 skipped files | 101,503,900 | 98,440,226 | 9,155,314 | 8,991,094 | 75,965 | 75,960 |
| 4,096 skipped files | 416,085,992 | 396,203,396 | 37,452,481 | 36,797,689 | 303,396 | 303,378 |
| 1,024 files, 8 written | 101,182,547 | 101,252,884 | 9,113,815 | 8,949,673 | 76,376 | 76,370 |
| 16 files, all written | 6,378,542 | 6,450,566 | 210,141 | 210,140 | 2,142 | 2,142 |

Allocation bytes drop 1.73%-1.80% for the larger unchanged/mixed fixtures
(`p=0.008`, `n=5`); one-file and all-written controls retain allocation counts.
The 4,096-skipped fixture allocates about 639 KiB fewer bytes per call.

**There is no statistically significant end-to-end SDK latency improvement
in these samples.** The 64-file fixture measures a 5.47% slowdown
(`p=0.008`). All other timing differences are not significant:
one file `p=0.690`, 1,024 skipped `p=0.056`, 4,096 skipped `p=0.690`,
mixed `p=1.000`, all-written `p=0.421`. Do not turn smaller observed
large-case medians into a speedup claim or dismiss the small-case result.

Separate diagnostic profiles are dominated by filesystem syscalls.
The original profile samples `sliceContains` at about 2.3% cumulative CPU;
the candidate no longer calls it on this path. Profiles include process
setup/fixture seeding and are not isolated timed-loop percentages or another
before/after latency comparison. Allocation improvements are not a peak-heap
or memory-cap claim.

All six fixture pairs retain five samples; benchstat warns that five do not
establish its 95% confidence intervals. Raw samples and ASCII-normalized
tables: `docs/issues/security-r5-materialize-2026-10-18/`.

```sh
CGO_ENABLED=0 go test ./pkg/harness-compilation -run '^$' \
  -bench '^BenchmarkMaterializeBookkeepingDiskIONoNetwork$' \
  -benchtime 2s -count 5 -benchmem
```

## Preservation And Negative Controls

- Public mixed-generation cases pass before and after: overwrite, sync,
  dry overwrite and dry sync. They assert ordered reports, caller input,
  actual existing/new-file updates, written modes, retained bytes/modes/
  inodes/mtimes, stale-file behavior and no stage residue.
- Skipped files deliberately request a different mode. Matching bytes
  still preserve their existing mode/inode/mtime rather than chmodding them.
- An all-unchanged generation still reaches root admission. Replacing the
  root at that boundary returns `fsboundary.ErrGenerationConflict` and
  preserves the old output and leaves no stage residue.
- Removing skipped paths from the all-path set fails both sync controls:
  retained files incorrectly appear in prune reports.
- Adding an all-skipped early return fails the root-refusal control.
- Retaining skipped files in the write list fails both real-write controls:
  an unchanged file is replaced/chmodded/rewritten.
- All three mutations are removed. The permanent public benchmark is the
  performance regression witness; no flaky wall-clock unit assertion or
  new runtime input limit is added.

Fresh integrated short-mode tests pass for all harness-compilation
packages, catalog and materializer, plus vet, zero-issue CI lint and
build. Six CLI regressions pass for already-synced output, prune with an
unrelated failure, committed config/lock retention on materialization
failure, native support files, additive hook selection and payload-free
logging. Focused SDK materialization/private-mode/consumer races pass.
Strict spec checks pass: 60 items, 1,035 scenarios and 1,657 annotations;
archive audit passes. Combined E2E and compatibility shell checks pass:
2,927 invocations, 113/113 command requirements and all three required
refusal witnesses.

Host OCR-delegation adversarial review covers all nine intended files,
including the eight default-excluded tests/documents/raw-data files.
Zero files are skipped and no actionable findings remain. These are scoped
local gates, not a full root-suite, vendor-native or released-client
container validation claim; neither review nor tests prove absence of
other defects.

## Remaining Work

The confirmed R35 skipped-list bookkeeping cost is addressed, not all
materialization, filesystem processing or #434. Actual file admission/read/
write/prune costs remain; other repeated-work candidates, read and parser
budgets and D1-D6 retain their separate obligations. No global processing
or memory bound follows from this change.
