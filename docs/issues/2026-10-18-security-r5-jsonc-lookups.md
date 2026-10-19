# Security R5: Ordered JSONC Object Patching (R43)

## Scope

This independent slice starts at main `32259476` while R47 PR #749 is
queued, then rebases onto `7d8c9e84` after it merges. SDK, JSONC, catalog,
materializer and module code are unchanged across that rebase.
Frozen R43/F108 (`01a0cecf-8430-7164-8781-21db1e72ade8`) concerns repeated
JSONC aggregate merge/patch work. The frozen ledger/source index remain
unchanged from `2f55f85f`.

No D1-D6 decision, parser acceptance, ownership/fragment semantics, ceiling,
truncation, source policy or output format changes. R38 highlighting and
other processing-budget obligations remain separate.

## Confirmed Cost And Change

The public `artifact.MergeWithExistingJSON` validates/parses/decodes input
before making merge decisions, then patches its concrete syntax.
`patchObject` previously copied the ordered keys and called `Node.Member`
for each; `Member` linearly searches the same member list. A wide MCP
container therefore incurred quadratic key comparisons during enumeration.

A five-second diagnostic profile of the original 4,096-entry unchanged
merger attributes 31.27% of CPU samples, cumulatively, to `Node.Member`.
The profile uses the original default 16-CPU setting and includes runtime
and fixture overhead; it is not an isolated phase or end-to-end profile.
Its text summary is retained with the measurements.

`Node.Members` now exposes read-only source-order key/value iteration using
the standard `iter.Seq2`. The merger consumes it after the existing
deletion phase and before sorted appends. The ordered syntax remains the
authority; keys retain their spelling/position and values may be replaced
without rebuilding a key lookup. Structural membership changes during an
iteration are explicitly unsupported.

There is no new index/cache, retained node state, shared read-time mutation,
node-size change or algorithm-switch threshold. `Keys`, single-key `Member`,
`SetMember`, `DeleteMember`, parsing, equality, report sorting, byte
rendering and publication retain their contracts.

SDK `Materialize` reaches this same merger for marker-owned aggregates;
native MCP planning also calls it from `internal/materialize`. This is a
shared pure-layer fix, not a separate workaround for one CLI path.

## Measurements And Limitations

Go 1.27.1, darwin/arm64, Apple M4 Max. The permanent benchmark
`BenchmarkMergeJSONCObjectLookupsCPU` includes parse/decode, merge decisions,
concrete-syntax patching and rendering of commented keyed MCP objects.
Fixture construction, preflight comparisons and compilation are excluded.
There is no disk or network I/O: these are public-merger CPU benchmarks,
not full SDK, CLI sync/update, browser or cold-acquisition measurements.

The initial sequential default-CPU pair is retained as preliminary data.
During the after-run, other E2E processes and heavy host security-tool
activity are observed. It shows 23-102% slowdowns in several small/medium
cases and no significant large-case gain, so it does not establish the
optimization's latency effect. It is not silently discarded or mixed
with the repeated comparison.

The repeat uses the same precompiled benchmark fixtures with `-test.cpu 1`.
For each fixture, before/after binaries alternate in pairs, reversing pair
order on alternating samples. Each side retains five two-second samples.
This reduces scheduling/drift confounding; it does not establish a
quiescent host or a production concurrency recommendation. No validation
is run by this task while either measurement series runs.

Interleaved medians:

| Members / update even indices | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| 1 / no | 9,343 | 9,792 | 6,937 | 6,937 | 135 | 135 |
| 1 / yes | 8,589 | 8,453 | 7,265 | 7,265 | 151 | 151 |
| 32 / no | 145,252 | 143,374 | 132,442 | 132,442 | 2,278 | 2,278 |
| 32 / yes | 155,018 | 154,240 | 137,675 | 137,675 | 2,538 | 2,538 |
| 256 / no | 1,141,832 | 1,096,883 | 1,041,660 | 1,041,659 | 17,327 | 17,327 |
| 256 / yes | 1,235,509 | 1,192,736 | 1,083,252 | 1,083,250 | 19,381 | 19,381 |
| 1,024 / no | 5,614,455 | 4,662,338 | 4,199,997 | 4,200,001 | 68,831 | 68,831 |
| 1,024 / yes | 5,919,446 | 4,964,860 | 4,345,794 | 4,345,788 | 77,032 | 77,032 |
| 4,096 / no | 34,473,699 | 19,285,858 | 16,795,851 | 16,795,787 | 274,875 | 274,874 |
| 4,096 / yes | 36,105,765 | 20,555,751 | 17,429,091 | 17,429,020 | 307,674 | 307,674 |

At 4,096 members, the controlled merger is about 1.8x faster (43-44% less
time); at 1,024 it takes 16-17% less time. Both comparisons have
`p=0.008`, `n=5`. The unchanged 256-member case improves 3.94%
(`p=0.016`); its updated counterpart is not significant.

Small 1/32-member timing changes are not significant. The one-entry
unchanged median is 4.8% slower (`p=0.841`), and is retained. Allocations
are effectively unchanged, including a one-allocation median difference
at 4,096 unchanged entries. Tiny byte/count differences are not claimed
as meaningful allocation savings. The iterator itself allocates zero
in its focused regression, without a new retained index.

Both comparisons retain all ten fixtures and five samples per fixture.
Benchstat warns that five samples do not establish its 95% confidence
intervals. Raw results, profile summary and ASCII-normalized tables are
in `docs/issues/security-r5-jsonc-lookups-2026-10-18/`.
Allocation bytes are not peak-heap or total-process memory limits.

The ordinary permanent benchmark command is:

```sh
CGO_ENABLED=0 go test ./pkg/harness-compilation/artifact -run '^$' \
  -bench '^BenchmarkMergeJSONCObjectLookupsCPU$' \
  -benchtime 2s -count 5 -benchmem
```

The interleaved comparison executes precompiled before/after binaries per
fixture, not a single sequential invocation of that command:

```sh
for size in 1 32 256 1024 4096; do
  for changed in false true; do
    for sample in 1 2 3 4 5; do
      if [ "$((sample % 2))" -eq 1 ]; then
        phases='before after'
      else
        phases='after before'
      fi
      for phase in $phases; do
        "./artifact-$phase.test" -test.run '^$' \
          -test.bench "^BenchmarkMergeJSONCObjectLookupsCPU$/^members-${size}$/^changed-${changed}$" \
          -test.benchtime 2s -test.count 1 -test.benchmem -test.cpu 1
      done
    done
  done
done
```

## Preservation And Negative Controls

- The wide public-merger test compares complete, independently authored
  output bytes and sorted reports for escaped keys, reverse-ordered
  handwritten entries, numeric spelling, a user collision, managed
  edits/removal, comment association and sorted appends. Repeat merge is
  byte-identical. It passes against the original and candidate merger.
- The public disk-SDK control passes with original/candidate patching,
  preserves dry-run bytes, performs an actual owned-entry update while
  retaining handwritten syntax/shared mode, then preserves output bytes,
  inode, mode and a deliberately old mtime on repeat. Attempt staging
  must be absent.
- Iterator controls pin decoded key/source order, values, repeated reads,
  early termination, empty/non-object values, concurrent read-only walks,
  value replacement during iteration and fresh reads after add/delete.
  Reads leave bytes and `Changed()` untouched; the enumeration guard
  requires all three members and zero allocations.
- Replacing direct iteration with the original key-copy/search pattern
  fails the allocation guard (`allocations=1`).
- Omitting the first member fails both public-merger byte preservation
  and the actual disk-SDK control. All mutations are removed.

Fresh integrated short-mode SDK/library, artifact, JSONC, catalog and
materializer tests pass, with vet, zero-issue CI lint and build.
Seven explicit CLI regressions pass: native MCP destinations,
ownership-proven/shared native hook sources, corrupt aggregate repair
refusal, owned MCP sibling retention, locked nested hook/MCP inventory,
and skipped Devin aggregate exit behavior.

Focused JSONC/artifact/SDK merge races pass. The latest during-iteration
value-replacement control and explicit shared-mode fixture setup are
freshly rerun under races, followed by zero-issue CI lint.
Strict specs pass: 60 items, 1,035 scenarios, 1,657 annotations and archive
audit. Combined E2E and compatibility shell checks pass: 2,927 invocations,
113/113 command requirements and all three required refusal witnesses.

Host OCR-delegation adversarial review covers all 16 intended files,
including 14 default-excluded test/doc/raw-data files, with zero skipped
and no actionable findings. These are scoped local gates, not a full
root-suite, vendor-native or released-client/container validation claim.
Neither passing tests nor review prove absence of other defects.

## Remaining Work

This addresses the confirmed lookup/enumeration component of R43, not
every JSONC merge/parse/patch cost or #434. Individual lookups, repeated
deletions and append mutation paths retain their existing algorithms;
decoding/canonicalization and broader parser/read/graph/memory budgets
remain separate. R43 stays partial. No global processing bound follows
from this change, and D1-D6 remain undecided.
