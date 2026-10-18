# Security R5 Execution: First Processing Batch

Status: in progress, first batch in PR #725; not closure of #434.

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
for the healthy API read. The original 16 benchmark variants and their
allocations remain in [before.txt](security-r5-2026-10-17/before.txt) and
[after.txt](security-r5-2026-10-17/after.txt). Those historical API samples
included request construction. The API row below instead uses the corrected
[before](security-r5-2026-10-17/api-before-no-request-setup.txt) and
[after](security-r5-2026-10-17/api-after-no-request-setup.txt) pair, both
measured with the same request construction outside the timed loop.

| Benchmark, largest fixture | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| Unterminated OSC, 2048 | 8,004,156 | 437,200 | 925,771 | 925,737 | 16,398 | 16,398 |
| Finding offsets, 2048 lines | 17,077,495 | 142,555 | 245,760 | 346,528 | 2,049 | 2,063 |
| Finding offsets, 2048 columns | 21,272,533 | 115,327 | 245,760 | 346,528 | 2,049 | 2,063 |
| JSON key validation, depth 512 | 1,517,947 | 152,568 | 18,611,644 | 421,449 | 1,584 | 1,083 |
| JSONC parsing, depth 512 | 1,648,988 | 223,403 | 18,373,171 | 173,161 | 2,569 | 2,062 |
| Healthy content digest route | 94,524 | 98,742 | 28,193 | 37,363 | 73 | 82 |

The offset ordering buffer increases allocated bytes by approximately 41%
at 2048 findings. Bounded filesystem reads increase healthy API allocations
by approximately 32.5% bytes and nine allocations; its timing difference is
not significant in these samples (`benchstat`, p=0.841). This is a safety
tradeoff, not a claimed API speedup. Five samples do not provide benchstat's
95% confidence interval; these are measured fixtures, not latency SLOs.

Scanner, converter and parser benchmarks measure in-memory work, not an
end-to-end refresh. The API benchmark includes local filesystem reads and
HTTP-handler dispatch, but no network transport, acquisition or owner lookup.
Request/fixture setup stays outside the timed loop. Per-response capture and
handler output remain timed.

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
# Compare the corrected API fixture separately, not against the old API row.
benchstat docs/issues/security-r5-2026-10-17/api-before-no-request-setup.txt \
  docs/issues/security-r5-2026-10-17/api-after-no-request-setup.txt
```

The before samples use the frozen source plus the same benchmark fixtures;
the after samples use the candidate implementations. The final scanner,
JSON and API samples replace intermediate candidate measurements. No
dependency versions change; `go.work.sum` records three missing checksums
for versions already selected by the workspace after the approved download.
The corrected API pair uses source baseline `2f55f85f` plus the updated
benchmark/fixture for before, and the PR implementation for after. Both use
five two-second samples with request construction excluded. The temporary
baseline worktree is a sibling and is removed before final validation.

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
- PASS: the distinct combined `make validate-e2e` runs below. Every run covers
  all 113 requirements with zero listed gaps, executes all three required
  launch refusal witnesses, and passes shellcheck. No duplicate standalone
  configuration-compatibility run. Vendor-native/container checks are not
  represented as local coverage.

| Run | Duration | Command invocations |
|---|---:|---:|
| Initial candidate, before legacy-error correction and rebase | 825 seconds | 2,874 |
| Legacy-error-corrected candidate, before rebase | 721 seconds | 2,874 |
| Publication candidate `fabf6176`, after rebase onto `f0171985` | 922 seconds | 2,881 |

The publication candidate also passes fresh scoped validation including
catalog, Marketplace client, materialization and source-revision consumers;
full-package races in JSON utilities, JSONC, analyze, ingest and API; and
strict spec checks with 945 scenarios and 1,537 annotations. The earlier
911/1,499 counts above describe pre-rebase validation, not this later run.

## PR Review Follow-Up

Gemini posts no actionable inline comments. CodeRabbit identifies request
construction in the timed benchmark and ambiguous E2E run labels; both are
corrected without changing production behavior. Original measurement data
remains available instead of silently comparing unlike benchmark fixtures.

The proposed revision-scoped inspection HTTP 413 mapping is not applied:
the original `f0171985` consumer returns HTTP 500 for an oversized inspection
manifest, and the spec requires existing refusal behavior. OpenAPI does not
establish a conflicting 413 contract for that route. A public-route matrix
covers GET/HEAD of the inspection, primary-content and digest-content routes,
healthy payloads, bounded refusal and legacy-store refusal. All preserve 500
for a manifest failure; built-in reads stop at the ceiling plus one byte.
Temporarily applying the proposed mapping fails all six bounded-route cases.
Follow-up checks pass: fresh full API package tests, scoped `make validate`
(vet, zero-issue CI lint and build), focused API races, and strict spec checks
(945 scenarios, 1,537 annotations). No production Go file changes in this
follow-up; the published candidate's E2E and released-client gates pass in CI.

The next CodeRabbit comment requests separate content-read coverage. The
manifest-refusal regression remains intact and now explicitly asserts that
only the manifest digest is requested. A separate 16-case matrix uses a
digest-aware probe with a valid manifest and matching content, exercising
GET/HEAD on primary and digest content routes with bounded and legacy stores.
It asserts the exact manifest-then-content request sequence, HTTP 200 at the
configured boundary, HTTP 413 above it, healthy bytes/media/cache identity,
no immutable refusal caching, and the bounded ceiling-plus-one read count.
Two deliberate mutations establish non-vacuity: invalid manifest data fails
all 16 cases before the content read; bypassing the bounded content reader
fails all eight bounded-store cases despite the post-read 413 still working.
Both mutations are removed; production code remains unchanged.
Fresh API package tests, scoped validation (vet, zero-issue CI lint and build),
focused API races and strict spec checks pass (945 scenarios, 1,538
annotations). OCR excludes both changed test/doc files by default; the host
reviews both manually, zero skipped, with no unresolved actionable
critical/high/medium findings.

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
the sibling-capacity and legacy-error corrections. The original implementation
delta has 28 files: all nine OCR-reviewable production/configuration files are reviewed,
and the 19 default-excluded tests, spec prose, dependency checksum and
measurement files receive supplementary manual checks. Zero files are
skipped; reviewable and overall coverage are 100%.
The published PR adds the two unchanged frozen artifacts, making 30 files
at `fabf6176`. The review follow-up changes five default-excluded files,
including two additional measurement files: all five receive manual review,
zero skipped, while production code stays unchanged. The full candidate
therefore contains 32 files, not a newly claimed 28-file PR review.
Neither this review nor passing tests proves absence of other defects.

## Remaining Work

All unlisted frozen rows still require their own current-consumer evidence.
R21 and R36 remain partial as described; R15/R18 and decisions D1-D6 are not
closed by these fixes. In particular, collection/include graph budgets,
finding-total/reject policy, archive aggregate bounds, discovery/search
buffering, Git/cache acquisition, and API query work retain their frozen
obligations. No new exclusion or invented threshold is introduced.

### Work Before D1-D6 Decisions

All 61 rows can be investigated now. Counting the frozen dispositions rather
than assuming every scanner claim is a defect, 31 rows have no explicit
decision tag; 30 mention a decision or new-budget caveat. This is a planning
split, not a count of confirmed vulnerabilities or guaranteed closures.

Of the 31, R03/R45/R54 have narrowly revalidated existing/removed controls,
and R16/R33/R57 have candidate fixes in this PR. That leaves 25 open/partial
candidates with no predeclared decision blocker:

| Candidate work | Cases | Safe work before a new policy decision |
|---|---|---|
| Repeated-work optimizations (9) | R28, R35, R38, R42, R43, R47, R48, R52, R53 | Measure and remove repeated serialization, parsing, scans or bookkeeping while preserving outputs, names, order, freshness and no-op metadata. |
| Read/owner-boundary checks (9) | R04, R09, R21, R31, R39, R40, R41, R58, R59 | Prove applicable existing limits and actual callers; enforce an already-established ceiling earlier or record a supported no-change result. |
| Other runtime/contract checks (7) | R12, R14, R19, R23, R24, R27, R44 | Verify cancellation, intentional operator overrides, existing acquisition limits, query/render costs and healthy controls before choosing a change. |

Missing an explicit tag does not approve a limit: if an investigation finds
that a new ceiling or behavior is necessary, that part still needs a decision.
Conversely, the other 30 rows are not entirely blocked. Semantics-preserving
optimization, context propagation under an existing contract, and enforcement
of established limits can proceed within them, as R36's path optimization
already demonstrates. New budgets and altered failure/output policy cannot.

The next independent slice addresses R48's `ProjectGitRoots` matching,
preserving selection, output order and refusal behavior without a new
admission limit. Its measurements, memory tradeoff and consumer evidence
are recorded in [the R48 execution report](2026-10-17-security-r5-lock-roots.md).
The 25-candidate count above describes this first batch's handoff, not a
claim that R48 or any other candidate was already closed by that batch.

#434 already tracks this remaining scope. Publication must describe this as
partial progress, preserve the frozen artifacts, and leave the issue open.
