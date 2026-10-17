# Security R5 Execution: First Processing Batch

Status: in progress, local implementation; not closure of #434.

The scope remains the [frozen ledger](2026-10-16-security-r5-frozen-ledger.md)
at `2f55f85f58a3fe3caca4965095bd90f8a1d82518`. Neither that ledger nor its
source index is edited by this batch. All other rows and decisions D1-D6
remain open unless separately evidenced. A candidate fix is not delivery or
integrated-main evidence.

## Candidate Fixes

| Case / aliases | Confirmed cost and change | Scope retained |
|---|---|---|
| R16, A69/F48 | Each unterminated OSC opener searched its remaining suffix. Cache only a proven absence of any later OSC terminator within the same scanner invocation. | Preserve every finding, source range, message and other terminal-control kind; do not close finding-total/reject policy. |
| R57, F162 | `toStoreFindings` rescanned the source prefix for each location. Group requests by their actual source, sort conversion requests, and use two forward rune scans without sorting emitted findings. | Preserve body/support routing, UTF-8 and invalid-byte behavior, repeated/out-of-order ranges, cross-line columns, EOF handling and stored finding order. R15/R18 remain separate. |
| R33, A110/F76 | Duplicate-key validation repeatedly copied ancestor keys into diagnostic paths. Retain segments in an invocation-local reusable stack and render paths only for errors. | Existing accepted depth, exact duplicate/alias behavior, paths, offsets and diagnostic codes remain unchanged. No new parser budget. |
| R36, A119/F84, partial | The JSONC reader shares the lazy diagnostic-path stack, eliminating the nested-long-key allocation cost. | A119's separate width/node/input-size claim is not closed by a path optimization; new parser budgets remain D1. Native bytes and accepted depths remain unchanged. |
| R21, A76/F55, partial | Content, inspection and registry/collection manifest readers fetched complete blobs before applying existing ceilings. Select the existing bounded-read backend capability before fetching. | Compressed archive delivery, placeholder/auxiliary archive reads and decompression remain open. Custom stores without the optional capability retain their compatibility path; they are not claimed bounded. |

The API uses the existing inspection-manifest, configured file and derived
manifest ceilings, not new thresholds. Built-in filesystem/S3 backends already
implement `GetBounded`. The production `serve` path obtains those concrete
backends from `config.OpenStores` and passes them directly to `api.NewServer`.

Nonpositive content ceilings and an unprobeable maximum-int64 ceiling retain
their previous behavior. Existing post-read checks remain for legacy custom
stores. Refusal statuses remain unchanged, including the existing generic
server error for an oversized registry manifest.

## Reproduction and Preservation

- The API byte-counting regression fails before the fix for all six global
  content/inspection/manifest GET/HEAD combinations: each selects unbounded
  `Get`, not `GetBounded`. Afterwards, reads consume at most the applicable
  ceiling plus one probe byte, retain refusal status and omit immutable
  success caching.
- Healthy content keeps its bytes, media type, verified digest and ETag/cache
  identity. Compatibility controls cover zero, negative and maximum-int64
  content ceilings and a custom store implementing only the legacy interface.
  Existing full API tests retain resource-inspection, manifest and archive
  behavior; the new byte-counting matrix is not an archive-read witness.
- Review also reproduced eight GET/HEAD cases where a legacy backend's
  wrapped read error was incorrectly reclassified as a new size refusal.
  Only failures from the selected bounded operation now receive that
  classification; legacy fallback errors retain HTTP 500, including
  unlimited/unprobeable reads. The original error cause stays wrapped.
- `BuildRegistryRevision` processes an inert, licensed local Git fixture and
  persists all 64 comment findings. Each stored byte range addresses its
  actual published primary inspection text. A frozen pre-optimization oracle
  independently checks malformed, reordered and cross-source location cases,
  including raw-byte fuzz seeds.
- Both JSON readers retain nested duplicate paths, array indices, escaped
  keys, diagnostic identity, offsets and exclusion of values from errors.
  A valid 512-level document with 128-byte keys remains accepted unchanged;
  the pre-existing excessive-depth refusal remains intact.
- Review found that the first candidate's slice-only path representation
  could still copy an ancestor stack for each sibling at capacity boundaries.
  The allocation regression reports 64 allocations across 64 siblings on
  that candidate. The shared stack passes the at-most-one-allocation check
  after warmup and preserves both parent and child path strings.

The semantic preservation tests also pass on the original algorithms where
applicable. Repeated-work evidence comes from the before/after benchmarks,
not timing thresholds in unit tests or a claim that preservation tests
distinguish algorithmic complexity.

## Existing Controls Revalidated

| Case | Current consumer evidence | Narrow disposition |
|---|---|---|
| R03, A17 | Six tests in `internal/config/bounded_read_test.go` exercise current root/include/overlay consumers, regular files, supported symlinks, special-file refusal and the existing 4 MiB cap. | No additional byte-limit fix. Recursive include total work remains R51/D2. |
| R45, F110 | `TestExtractDescriptionBoundsUntrustedContent`, `TestPutResourceRevisionBoundsDescription`, and `TestSearchBoundsHostileDescriptionOnEgress` pass freshly. | Retain the frozen no-change disposition for the stored/served description's existing 1024-byte cap only; not aggregate preview cost. |
| R54, F140 | `TestConsumerMigrationSurfaceIsAbsent` passes freshly through the shipped command surface. | Retain the frozen removed-consumer disposition. Do not restore a legacy decoder or imply that older releases are repaired. |

## Measurements

Go `go1.27.1`, `darwin/arm64`, Apple M4 Max. Each cell is the median of five
two-second samples. Fixtures are inert and bounded: counts 128/512/2048 for
scanner/offset work, depths 32/128/512 for JSON paths, and 7,360 content bytes
for the healthy API read. All 16 benchmark variants and their allocations
are retained in [before.txt](security-r5-2026-10-17/before.txt) and
[after.txt](security-r5-2026-10-17/after.txt).

| Benchmark, largest fixture | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| Unterminated OSC, 2048 | 8,004,156 | 437,200 | 925,771 | 925,737 | 16,398 | 16,398 |
| Finding offsets, 2048 lines | 17,077,495 | 142,555 | 245,760 | 346,528 | 2,049 | 2,063 |
| Finding offsets, 2048 columns | 21,272,533 | 115,327 | 245,760 | 346,528 | 2,049 | 2,063 |
| JSON key validation, depth 512 | 1,517,947 | 152,568 | 18,611,644 | 421,449 | 1,584 | 1,083 |
| JSONC parsing, depth 512 | 1,648,988 | 223,403 | 18,373,171 | 173,161 | 2,569 | 2,062 |
| Healthy content digest route | 97,584 | 104,503 | 33,506 | 42,687 | 82 | 91 |

The offset ordering buffer increases allocated bytes by approximately 41%
at 2048 findings. Bounded filesystem reads increase healthy API allocations
by approximately 27% bytes and nine allocations; its timing difference is
not significant in these samples (`benchstat`, p=0.222). This is a safety
tradeoff, not a claimed API speedup. Five samples do not provide benchstat's
95% confidence interval; these are measured fixtures, not latency SLOs.

Scanner, converter and parser benchmarks measure in-memory work, not an
end-to-end refresh. The API benchmark includes local filesystem reads and
HTTP-handler dispatch, but no network transport, acquisition or owner lookup.
Setup stays outside the timed loop.

Reproduce the comparisons from the repository root:

```sh
CGO_ENABLED=0 GOPROXY=off go test -p 1 \
  ./pkg/marketplace/analyze ./pkg/marketplace/ingest \
  ./pkg/harness-compilation/internal/jsonutil ./pkg/harness-compilation/jsonc \
  ./pkg/marketplace/api \
  -run '^$' \
  -bench '^(BenchmarkUnterminatedOSC|BenchmarkStoreFindingOffsets|BenchmarkNestedKeyValidation|BenchmarkNestedKeyParsing|BenchmarkContentDigestRead)$' \
  -benchtime 2s -count 5 -benchmem
benchstat docs/issues/security-r5-2026-10-17/before.txt \
  docs/issues/security-r5-2026-10-17/after.txt
```

The before samples use the frozen source plus the same benchmark fixtures;
the after samples use the candidate implementations. The final scanner,
JSON and API samples replace intermediate candidate measurements. No
dependency versions change; `go.work.sum` records three missing checksums
for versions already selected by the workspace after the approved download.

## Verification

- PASS: scoped `make validate`, selecting all harness-compilation packages,
  analyze, ingest, API, worker and store packages, with fresh tests; vet,
  non-mutating CI lint (zero issues) and CLI build pass. This is not a
  full-root test-suite claim.
- PASS: pinned, offline `make spec-check`, including strict validation,
  scenario trace (911 scenarios, 1499 annotations) and archive checks
  (two untouched active changes). The API delta has two annotated
  scenarios; algorithm-only commits retain no-impact explanations.
- PASS: focused `-race` tests in both JSON readers, analyze, ingest and API,
  including the public ingest and byte-counting HTTP controls.
- PASS: the config, description and removed-migrator controls listed above.
- PASS: combined `make validate-e2e`; 2,874 command invocations cover all
  113 requirements with zero listed gaps, and all three required launch
  refusal witnesses execute and pass. Shellcheck passes. No duplicate
  configuration-compatibility run. Vendor-native/container checks are not
  represented as local coverage.
- PASS: repeated scoped validation, API race tests and the combined E2E
  gate after the legacy-error correction. The final E2E run takes 721 seconds
  and again covers all 113 requirements and three required witnesses.

## Adversarial Review

The host agent uses `open-code-review-delegate` for deterministic selection
and rule resolution; no external model or subagent performs this review.
The supported production/configuration checklist is:

- [x] `pkg/marketplace/analyze/scanners.go` (modified): suffix proof, later
  control visibility and invocation-local state.
- [x] `pkg/marketplace/ingest/ingest.go` (modified): all conversion callers,
  source identity, ordering, invalid UTF-8, cross-line columns and overflow.
- [x] `pkg/harness-compilation/internal/jsonutil/jsonutil.go` (modified):
  both validator roots and immediate, owned diagnostic strings.
- [x] `pkg/harness-compilation/internal/jsonutil/path.go` (added): reusable
  capacity, scratch lifetime and sibling/ancestor aliasing.
- [x] `pkg/harness-compilation/jsonc/jsonc.go` (modified): one stack per parse.
- [x] `pkg/harness-compilation/jsonc/parse.go` (modified): all recursive and
  error paths; no retained scratch views in parsed documents or errors.
- [x] `pkg/marketplace/api/blob_reads.go` (added): concrete runtime backends,
  context propagation, positive/probeable limits and legacy compatibility.
- [x] `pkg/marketplace/api/server.go` (modified): all affected callers,
  not-found/size status, availability/identity checks and caching on refusal.
- [x] `openspec/changes/archive/2026-10-17-bound-marketplace-payload-reads/.openspec.yaml`
  (added): recognized schema keys and change metadata.

No unresolved actionable medium/high finding remains in these changes after
the sibling-capacity and legacy-error corrections. The intended delta has
28 files: all nine OCR-reviewable production/configuration files are reviewed,
and the 19 default-excluded tests, spec prose, dependency checksum and
measurement files receive supplementary manual checks. Zero files are
skipped; reviewable and overall coverage are 100%.
Neither this review nor passing tests proves absence of other defects.

## Remaining Work

All unlisted frozen rows still require their own current-consumer evidence.
R21 and R36 remain partial as described; R15/R18 and decisions D1-D6 are not
closed by these fixes. In particular, collection/include graph budgets,
finding-total/reject policy, archive aggregate bounds, discovery/search
buffering, Git/cache acquisition, and API query work retain their frozen
obligations. No new exclusion or invented threshold is introduced.

One next ungated candidate is R48's `ProjectGitRoots` matching. Its current
selection order prefers the first explicit harness match, then the first
already-selected match for an implicit requirement, otherwise the last
matching root. Any indexing optimization needs to preserve those choices,
the original output order and refusal behavior, with fresh measurements.

#434 already tracks this remaining scope. Publication must describe this as
partial progress, preserve the frozen artifacts, and leave the issue open.
