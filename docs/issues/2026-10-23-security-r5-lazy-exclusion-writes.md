# R5 Lazy Exclusion Writes

## Scope And Contract

This independent R53/F136 slice starts from main `656ef4db` and integrates
QA-only main `736e4779`. The measured production/dependency bytes are
unchanged by that integration. R53, R5, and #434 remain partial; neither
the frozen ledger nor its source index changes.

Both v4 request interpreters previously inserted a false exclusion marker
for every explicit include. They now skip that write while their local
exclusion map is empty. Empty-map boolean lookups are already false.
Once any exclusion has populated the map, the existing false assignment
remains, including after repeated exclusion/re-inclusion.

The maps are local and are not exposed, enumerated, or tested with the
comma-ok form. No request, authored decision, matching rule, required
protection, fallback, provenance, harness, ordering, scope, diagnostic,
file/lock/publication behavior, or accepted input changes. No new cap,
deadline, persistent cache, or index is introduced.

## Method And Source Identity

The evidence directory is `security-r5-lazy-exclusions-2026-10-23/`.
Its top-level files describe the delivered guard; `rejected-delete/`
contains a different, rejected implementation. Do not pool those datasets.
The initial three-sample probes are not used for significance claims.

Each paired dataset contains 432 rows: 36 cases, two variants, six samples
per case/variant. Trials alternate before/after and after/before. The
same test/benchmark definitions are compiled into both binaries, replacing
only the original production source through a physical-path Go overlay.
Source/binary hashes are checked during the run; both manifests retain
the exact source, binary, dependency, compiler, and trial identities.
All 864 rows and case/sample identities are checked before publication.

- Go 1.27.3, darwin/arm64, Apple M4 Max.
- One second per benchmark case, `GOMAXPROCS=16`, `GOGC=100`,
  `GOMEMLIMIT=off`, `CGO_ENABLED=0`, and offline module/toolchain selection.
- Original production blob: `a9cc6e5b55b0101196079704cbdff4da5a872915`.
- Delivered production blob: `2f57ba35ed90d93486abe7940ad6b5096b5e146b`.
- Delivered benchmark blob: `189d6f675300b941974e3e8ecc7231d225b6f9b1`.
- Compiled disassembly shows the original unconditional false assignment
  and the delivered zero-length check branching around that assignment
  in both changed functions. The rejected variant instead calls map deletion.

The rejected benchmark source differs from the delivered definition only
in its two-line churn comment (the former described deletion/reinsertion).
Its manifest retains that original hash rather than relabeling it.

All timed work is CPU-only. Rebuild/effective interpretation exclude fixture
parsing; public parsing includes parsing; public runtime adaptation excludes
parsing. None includes disk/network work. Fixtures use 1, 256, or 1,024
skills, with zero, half, or all excluded, plus an unrelated retained command.
The tiny half-excluded duplicate is omitted. Assertions run after `b.Loop`.
The churn control expands 64 or 1,024 exclusion/re-inclusion rounds for one
locator; it does not measure include-tree parsing or disk reads.

## Delivered Results

Selected 1,024-skill medians; p-values are nominal six-sample comparisons,
not adjusted across the complete table. Complete time/byte/allocation
results, including the smaller cases, remain in `benchstat.txt`.

| Path | Excluded | Before | After | Time Change |
|---|---:|---:|---:|---:|
| Request rebuilding | 0 | 362.5 us | 297.3 us | -17.98%, p=0.002 |
| Request rebuilding | 512 | 311.7 us | 280.7 us | -9.97%, p=0.002 |
| Public parsing | 0 | 1.316 ms | 1.249 ms | -5.04%, p=0.002 |
| Public parsing | 512 | 1.749 ms | 1.721 ms | Not significant, p=0.240 |
| Public parsing | 1,024 | 2.133 ms | 2.142 ms | Not significant, p=0.699 |
| Effective interpretation | 0 | 243.2 us | 191.3 us | -21.36%, p=0.002 |
| Public runtime adapter | 0 | 324.0 us | 273.5 us | -15.59%, p=0.002 |
| Public runtime adapter | 512 | 572.8 us | 509.9 us | -10.98%, p=0.002 |

For healthy 1,024-skill fixtures:

| Path | Before B/op | After B/op | Before Allocs/op | After Allocs/op |
|---|---:|---:|---:|---:|
| Rebuilding | 1,537,363 | 1,428,603 | 1,090 | 1,070 |
| Public parsing | 2,666,110.5 | 2,557,128.5 | 15,346 | 15,325 |
| Effective interpretation | 662,144 | 553,384 | 1,067 | 1,047 |
| Public runtime adapter | 1,083,786 | 975,025.5 | 3,136 | 3,116 |

Half-excluded helper fixtures save about 54.6 KB; the runtime adapter saves
about 109.2 KB because it interprets the decision stream more than once.
Complete-exclusion allocation counts are unchanged. The complete-exclusion
public parser has a measured +19.5 B/op median variation (p=0.009); the
adapter varies by -1.5 B/op (p=0.022). Neither tiny difference is presented
as a practical memory regression or an allocation win for that density.

The large churn controls show no significant timing difference: rebuilding
106.9 us versus 106.9 us (p=0.937), effective interpretation 42.00 us versus
41.84 us (p=0.310). Their allocation counts/bytes are unchanged. The smaller
rebuild churn control is also not significant; the effective control is
1.37% faster. No universal or end-to-end I/O improvement is claimed.
After the first actual exclusion, later includes may still add false map
entries; the change does not guarantee sparse storage for every interleaving.

Allocation profiles attribute over 99.8% of the narrow rebuild benchmark's
allocated space to rebuilding. Total profile bytes are not compared because
the faster variant executes more operations; the paired B/op is the
per-operation evidence.

## Rejected Deletion Variant

Deleting every re-included marker produces similar healthy allocation
savings but regresses repeated exclusion/re-inclusion. Its separate paired
results show:

| Control | Before | Delete | Time Change |
|---|---:|---:|---:|
| Rebuild, 64 rounds | 7.242 us | 7.585 us | +4.74%, p=0.004 |
| Effective, 64 rounds | 2.901 us | 3.326 us | +14.67%, p=0.002 |
| Rebuild, 1,024 rounds | 107.6 us | 114.7 us | +6.63%, p=0.002 |
| Effective, 1,024 rounds | 42.20 us | 48.86 us | +15.77%, p=0.002 |

The delivered guard retains the existing clearing assignment after the
map is populated, instead of shipping that deletion/reinsertion tradeoff.

## Preservation And Validation

Both original and delivered compiled variants pass the request-rebuilding
and effective-request tests with 1,000 generated cases per oracle. The
tests compare complete requests/decisions and unchanged input groups.
New cases cover repeated exact/global/kind re-inclusion and a subsequent
registry-kind exclusion. Existing consumers exercise include trees,
runtime adaptation, intent selection, and Marketplace acquisition locks.

Six private-overlay mutations fail both their unit witness and the public
include-tree/runtime-adapter witness: omitting the reset, clearing the
wrong key, and inverting the guard, separately in both interpreters.
The focused race run passes in 3.584 seconds. On the integration with
QA-only main `736e4779`:

- Explicit offline `GOWORK=off go mod tidy -diff` passes without output
  or dependency-file changes.
- Selected config/pipeline/resolver/TUI validation, vet, zero-issue lint,
  and build pass. This is not a local full-root-suite claim.
- Strict specs pass 66 items, 1,198 scenarios, 1,961 annotations, and two
  active changes.
- All 59 terminal journeys plus four utilities pass, with zero skips;
  dependency installation uses the locked offline cache without downloads.
- Fresh combined E2E/configuration validation passes in 755.272 seconds:
  3,427 invocations, all 113 requirements, no gaps, and all three required
  refusal witnesses executed and passed, followed by included shellcheck.

Host `open-code-review-delegate` review accounts for all fourteen own
files, including the OCR-excluded tests/raw evidence/documentation and
both manifests. Code/consumer reachability, boolean-map equivalence,
mutations, fixture honesty, all 864 unpooled rows, source identities,
and rejected-variant/tiny-byte qualifications are checked. Zero files
are skipped; no actionable finding remains.

## Reproduce

On an equivalent toolchain, compare the original production source from
`656ef4db:internal/config/v4.go` with the delivered source using identical
benchmark definitions. Keep each variant's output separate:

```sh
go test ./internal/config -run '^$' \
  -bench '^(BenchmarkV4FilterSortRebuildCPU|BenchmarkParseV4FilterSortCPU|BenchmarkV4SparseExclusion(Effective|Adapter|Churn)CPU)$' \
  -benchtime=1s -count=6 -benchmem
benchstat before.txt after.txt
```

For interleaving, compile each variant once with `go test -c`, use
`-test.count=1`, and alternate the two binaries for six trials. Match the
manifest settings and verify all 36 identities and six samples per variant.
