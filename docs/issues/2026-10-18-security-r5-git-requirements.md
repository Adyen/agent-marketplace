# Security R5: Repeated Git Root Requirements (R47)

## Scope

This independent slice starts at main `ced6f887`, separate from open R35
PR #745. Frozen R47/F116 (`01a0cecf-8433-75bf-a491-ff75b4b100c5`)
concerns repeated source-revision builds during lock acquisition.
The frozen ledger/source index remain unchanged from `2f55f85f`.

No D1-D6 decision, new ceiling, quota, deadline, truncation, source policy
or lock/source-revision format is introduced. R38 preview highlighting and
R43 JSONC patching remain separate.

## Confirmed Cost And Change

`reconcileLayerDocument` groups canonical repository/ref requirements within
one layer. `acquireLockedRegistry` resolves the ref and acquires one exact
snapshot, but previously built every requested source revision before
discarding duplicate output roots. Duplicate aliases therefore repeated
ingestion, discovery, content/support hashing and archive work.

An invocation-local set now records successful builds by the existing
domain type `lockfile.GitRootRequirement`: normalized root path plus the
exact requested `SourceHarness`. Only empty paths become `"."`, as before.
Repeated identical inputs skip the build; the existing separate
root-path/resolved-harness output set still controls first-occurrence
inventory and ordering.

Auto-detection and an explicit hint remain independent inputs even if both
resolve to Claude. Different explicit harnesses and root paths remain
independent too. Repository, ref, snapshot, default revision schema and
scope retain the function's existing invocation boundaries. There is no
persistent or cross-operation cache and no cached failed build.

Ref resolution, snapshot acquisition/lease/verification, downstream exact
inventory verification, grouping, policy selection, publication and coded
refusals are unchanged. The same path serves lock creation and selected
upgrade acquisition for CLI consumers and their shared orchestration.

## Measurements

Go 1.27.1, darwin/arm64, Apple M4 Max. Identical benchmark source/fixtures;
sequential `-benchtime 2s -count 5 -benchmem` runs without concurrent local
validation. A real CLI seed sync creates the exact lock/cache from a local
bare Git origin containing one skill plus a support script.

Timed acquisition includes local Git subprocesses, ref resolution, exact
cache/snapshot checks, source ingestion, hashing and inventory construction
on disk. Fixture creation/seed sync, preflight output comparisons,
compilation and external network are excluded. These are warmed acquisition
measurements, not complete CLI sync/update or cold acquisition timings.

Observed medians:

| Requirements | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| 1 implicit | 242,979,963 | 245,399,269 | 952,553 | 949,013 | 3,642 | 3,640 |
| 16 repeated implicit | 309,084,083 | 241,433,620 | 4,879,532 | 963,428 | 34,587 | 3,641 |
| 128 repeated implicit | 793,109,764 | 243,024,657 | 34,088,170 | 975,120 | 265,606 | 3,640 |
| 512 repeated implicit | 2,466,713,333 | 235,432,729 | 134,129,312 | 996,195 | 1,057,656 | 3,644 |
| 128 repeated explicit | 705,623,694 | 243,709,292 | 30,255,720 | 970,858 | 226,673 | 3,338 |
| 2 distinct policies | 251,050,745 | 242,309,157 | 1,205,622 | 1,220,092 | 5,046 | 5,051 |

At 512 repeats, acquisition is about 10.5x faster, with 99.3% fewer Go
allocation bytes and 99.7% fewer allocations. At 128 repeats, implicit/
explicit acquisition is about 3.3x/2.9x faster. Repeated-case timing and
allocation comparisons have `p=0.008`, `n=5`.

Single-request timing measures about 1% slower (`p=0.421`); the distinct
policy control measures about 3.5% faster (`p=0.421`). Neither timing
change, nor their allocation-metric differences, is significant in these
samples. Distinct-policy bytes measure about 1.2% higher (`p=0.151`);
retain that observation rather than claiming unchanged allocations.

All six pairs retain five samples; benchstat warns that five do not establish
its 95% confidence intervals. Raw data and ASCII-normalized tables:
`docs/issues/security-r5-git-requirements-2026-10-18/`.

An additional candidate allocation profile is diagnostic only: it includes
process/test initialization and seed sync, not solely timed acquisition.
Go allocation metrics exclude Git child-process memory, and reduced B/op
is neither a peak-heap nor total-process-memory-cap claim. The set retains
keys proportional to unique requested root/policy pairs during one call.

```sh
CGO_ENABLED=0 go test ./cmd/agent-marketplace -run '^$' \
  -bench '^BenchmarkAcquireRepeatedGitRequirementsLocalDiskNoNetwork$' \
  -benchtime 2s -count 5 -benchmem
```

## Preservation And Negative Controls

- Real acquisition controls compare complete inventory against the CLI's
  committed seed lock, including commit, schema/digest, resource content/
  archive digests and support-file inventory. A literal fixture revision
  digest independently guards the seed. Implicit/explicit hints in both
  orders still build twice despite one resolved output root; distinct
  Claude/Factory policies retain both ordered roots.
- The original duplicate regression observes 32 real build-debug records
  for 32 equivalent requests and fails. The candidate observes one, for
  implicit and explicit requests, empty/dot path aliases and two separate
  invocations. No fake builder or new production test hook is introduced.
- Distinct root paths retain independent inventory. Traversal and invalid
  hints preserve `ErrPathEscape`/`ErrCatalog` and return no partial registry;
  initially canceled calls preserve `context.Canceled`.
- A real v4 CLI config with three source aliases passes update, locked sync
  and check while preserving exact committed lock bytes and installed
  output. Baseline policy/refusal/CLI preservation controls pass before the
  optimization as well.
- Omitting requested policy from the set key fails all three policy
  controls and hides the invalid-policy refusal.
- Omitting root path fails the distinct-root control. Using the raw rather
  than established normalized empty/dot path fails the live build-count
  regression (two builds instead of one).
- All mutations are removed. Counts are regression assertions only, not
  accepted-input caps; the 512-request fixture is processed fully.

Integrated gates and host OCR-delegation review are pending.

## Remaining Work

The confirmed R47 duplicate-build cost is addressed, not all acquisition,
source revision processing or #434. Unique-root ingestion, archives,
read/parser and graph budgets, remaining repeated-work candidates and
D1-D6 retain their separate obligations. No global processing or memory
bound follows from this change.
