# R5 Request References During Selection Replay

## Scope And Preservation

This is an R53/F136 allocation slice under performance tracker #765, delivered
on main `027056e3`. R53, R5, #765, and the security track remain partial.
The frozen ledger and source index are unchanged.

Both v4 selection interpreters used full request structs as local map values.
Replay now keeps references to existing requests and emits value copies of
retained requests. Include-only fallback addresses come from the existing
slice by index. The maps and references remain local to each invocation.

Required protection, exact/global/kind/registry exclusions, replacement,
re-inclusion, nil requests, include-only fallback distinctions, complete
request metadata, provenance, source-group and lexical ordering, authored
decisions, input immutability, and nil/empty results retain their contracts.
Parsing, accepted input, diagnostics, scope, acquisition locks, ownership,
and publication behavior are unchanged. No limit or policy is introduced.
The commit carries a justified `Spec-Impact: none` trailer.

## Two Separate Measurement Baselines

The evidence directory is `security-r5-request-references-2026-10-23/`.
Its top-level dataset measures main `4dfa9dae`: 39 cases, six samples per
variant, 468 rows. Its `integrated/` dataset measures the public Git runtime
adapter after effective-request reuse, on main `027056e3`: eight cases,
six samples per variant, 96 rows. Do not pool these datasets.

The initial matrix covers 1, 256, and 1,024 skills, with zero, half, or all
skills excluded, plus an unrelated retained command. It also covers 64 and
1,024 exclusion/re-inclusion rounds and include-only fallback rebuilding.
The duplicated tiny half-excluded case is omitted. The fallback benchmark
exercises the internal rebuilding contract, not include-tree parsing.

Main integration changes adapter call counts. Both replay function bodies
are byte-identical to their initially measured counterparts; their hashes
are retained in the integrated manifest. Adapter figures for the delivered
base come only from the integrated dataset.

Both comparisons use identical test/benchmark definitions within each
before/after pair and immutable compiled binaries. The initial original is
compiled before the code changes. The integrated original uses a physical-
path Go overlay replacing only production source with `027056e3`.
The original allocation guard fails in all three modes, proving that the
overlay actually selected the original bookkeeping. Source, dependency,
test, binary, and result hashes identify each dataset. All case identities,
sample counts, PASS footers, and binary identities are checked.

Go 1.27.3, darwin/arm64; `CGO_ENABLED=0`, `GOMAXPROCS=16`, `GOGC=100`,
`GOMEMLIMIT=off`. Six trials alternate AB/BA with two-second samples.
Fixture construction is outside timing. Only the public parser benchmark
includes parsing; replay and adapter benchmarks exclude parsing. All timed
fixtures exclude disk and network I/O. Initial probes and allocation profiles
are separate from the paired samples.

## Allocation Results

Selected medians for 1,024 skills plus one retained command. The first four
rows use the initial dataset and unchanged replay bodies; the final three
rows use the integrated adapter baseline.

| Path | Skills Excluded | Before B/op | After B/op | Before Allocs/op | After Allocs/op |
|---|---:|---:|---:|---:|---:|
| Rebuilding | 0 | 1,428,602.5 | 1,215,402 | 1,070 | 45 |
| Effective interpretation | 0 | 553,386 | 340,184 | 1,047 | 22 |
| Public parsing | 0 | 2,557,100 | 2,343,898 | 15,325.5 | 14,300 |
| Include-only fallback | 0 | 1,036,058 | 822,858 | 1,058 | 33 |
| Integrated Git adapter | 0 | 975,026 | 761,826 | 3,116 | 2,091 |
| Integrated Git adapter | 512 | 1,082,702.5 | 869,499.5 | 4,190 | 3,165 |
| Integrated Git adapter | 1,024 | 1,211,765 | 998,562 | 5,216 | 4,191 |

Each integrated large adapter case saves 1,025 allocations and about 213 KB
per operation. The initial pre-reuse adapter interprets excluded groups
more than once; its larger savings do not describe the delivered adapter.
Large churn controls save two allocations and about 416 bytes per operation;
their output and complete decision history are retained.

Both complete `benchstat.txt` files retain time, bytes, and allocations for
every case, including small inputs and complete exclusion. Timing intervals
are wide (up to 448% in the initial matrix), and p-values are nominal,
unadjusted comparisons. Timing medians and geomeans do not establish a
universal or end-to-end latency improvement. Allocation reduction is the
primary result. Every paired sample and outlier is retained.

The original allocation profile attributes the majority of effective-replay
bytes to map insertion of request structs. The changed profile still includes
output and map-container allocation. Profile totals are not compared: the
number of measured operations differs. Paired B/op and allocation counts
provide the per-operation evidence.

## Correctness And Verification

The allocation-scaling guard fails on the original decision replay in both
interpreters and on include-only fallback rebuilding. It allows container
growth while rejecting an allocation for each retained request; it has no
wall-clock threshold. Independently authored value-copy controls also prove
that input and returned scalar fields remain independent.

Both compiled variants pass the per-locator preservation oracles at 1,000
cases before and after main integration. Existing public include-tree,
runtime/intent, and Marketplace-lock witnesses retain complete selections
and refusal/preservation behavior.

Private physical-path mutations replace the fallback index or the decision
request pointer in each interpreter. All three compile and fail their
independently authored value-copy witness. Decision-pointer mutations also
fail the public include-tree/runtime/intent/Marketplace-lock witnesses.
The tracked production bytes remain unchanged.

- Focused integrated config races pass (5.851 seconds).
- Offline `GOWORK=off go mod tidy -diff` reports no dependency changes.
- Selected `make validate` passes config, pipeline, resolver, and TUI tests,
  vet, CI-pinned zero-issue lint with a fresh cache, and build. This is a
  scoped local result; CI owns the full root suite.
- Strict specs pass 66 items, 1,199 scenarios, 1,966 annotations, and the
  archive audit with two existing active changes.
- Terminal QA passes all 62 tests and four utilities, with zero failures or
  skips, using the pinned Node/pnpm toolchain and frozen offline dependencies.
- The initial combined local E2E attempt, run alongside terminal QA,
  reached the 20-minute package deadline and the wrapper reported only a
  package failure (1,224.679 seconds including setup).
- An unfiltered host retry with isolated Git configuration and a 40-minute
  local package deadline passes 1,446 tests and subtests, with no failures,
  in 1,304.308 seconds. All three required refusal witnesses pass. Command
  coverage records 3,427 invocations, 113 of 113 requirements, and zero
  listed gaps; ShellCheck passes both compatibility scripts. This does not
  change the repository's 20-minute deadline or count the earlier attempts
  as passing. Nine existing skips remain: five hook defects tracked by
  #814, #806, #810, #813, and #631, plus four tests reserved for the dedicated
  released-client gate. These skipped cases are not counted as passed.
  Standard E2E/released-client acceptance and the full root suite must pass
  in PR CI.

## Review Coverage

Model-backed OCR reviews the production file and both JSON manifests:
three distinct selected files, zero findings. Their exact reviewed bytes
are checked before publication. Its ten default exclusions are:

```text
internal/config/v4_request_replay_test.go
internal/config/v4_request_replay_bench_test.go
docs/plans/2026-10-23-r5-request-references-design.md
docs/issues/2026-10-23-performance-r5-request-references.md
docs/issues/security-r5-request-references-2026-10-23/before.txt
docs/issues/security-r5-request-references-2026-10-23/after.txt
docs/issues/security-r5-request-references-2026-10-23/benchstat.txt
docs/issues/security-r5-request-references-2026-10-23/integrated/before.txt
docs/issues/security-r5-request-references-2026-10-23/integrated/after.txt
docs/issues/security-r5-request-references-2026-10-23/integrated/benchstat.txt
```

Host `open-code-review-delegate` accounts for all 13 own files, including
those exclusions: 13 reviewed, zero skipped, 100% coverage. It checks
consumer reachability, nil-pointer admission, invocation-local reference
lifetime, scalar value-copy semantics, existing nested-slice sharing,
required/exclusion/re-inclusion/fallback precedence, ordering, refusal
behavior, and unchanged acquisition, ownership, scope, and write boundaries.
The authored regression controls and all 564 unpooled measurement rows,
source identities, trial orders, and published hashes are verified.
No actionable critical/high/medium finding remains in the reviewed change;
passing tests and clean reviews do not establish absence of every defect.

Reviewed production blob: `691425dc80642c87000ecf427cb613ca59c79ea2`.


## Reproduce

On the delivered base, compare `027056e3` with the candidate using identical
benchmark definitions and the same environment:

```sh
go test ./internal/config -run '^$' \
  -bench '^BenchmarkV4SparseExclusionAdapterCPU$' \
  -benchtime=2s -count=6 -benchmem
benchstat before.txt after.txt
```

For paired trials, compile each variant once with `go test -c`, use
`-test.count=1`, and alternate AB/BA for six trials. Follow the manifest's
source and environment identities.

To reproduce the historical matrix, start from `4dfa9dae`, copy the two
new request-replay test/benchmark files from the candidate, and compare the
original source with only this PR's production diff applied. Its other
benchmark definitions already exist at that baseline. Use the top-level
manifest's six-family benchmark selector. Do not combine its adapter
samples with the integrated comparison.
