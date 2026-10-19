# Security R5: Raw Preview Boundary Lookups (R38)

## Scope

This independent slice starts at main `f44101fa`, after R43 PR #751
merged. Frozen R38/A128/F91
(`01a0a06b-ae77-714a-9334-606e9a37a6f4`) concerns repeated raw-preview
highlight/render work through the actual browser consumer. The frozen
ledger/source index remain unchanged from `2f55f85f`.

No new policy ceiling, truncation, parser acceptance, rendering policy,
source policy or D1-D6 decision changes. The API's existing 1,000-finding
admission remains unchanged; the browser fixture has 962 findings.

## Confirmed Cost And Change

`ResourceDetailView` reads the `useResourceInspectionPreview` composable's
`highlightedContent` when displaying escaped raw inspection text.
`highlightEscapedRawText` builds ordered UTF-8 byte/UTF-16 string
boundaries, then previously searched the whole array twice per finding.
Lookup comparisons therefore grew with source length times range count.

Only `byteBoundary` changes: use exact-match binary search over the same
monotonic array. No new map, cache, dependency, threshold, boundary-building
representation or reactive state is added. `TextEncoder` construction,
sanitization, range sorting/filtering, overlap grouping, escaping, IDs,
labels, HTML generation, line/column fallback and Vue caching are unchanged.

An index map would add per-render storage; an endpoint sweep would add
sorting/mapping machinery. Binary search reuses the existing ordered data
with a small local change. This addresses repeated lookup work, not all
source decoding, character allocation, rendering or DOM cost.

## CPU Measurements And Limitations

Node v24.21.0, Vitest 4.1.11, darwin/arm64. The permanent
`web/tests/unit/markdown.bench.ts` uses the installed Vitest benchmark
runner. No new dependency or Go wrapper is introduced.

The benchmark includes sanitization, UTF-8 boundary construction, finding
validation/sorting/grouping, escaping and output construction. Setup,
preflight checks, compilation, DOM, frames, disk and network are excluded.
This is public-helper CPU timing, not whole-browser or CLI latency.

Before any production change, capture five separate benchmark processes,
each retaining a two-second window per fixture after warmup. Repeat the
same command and unchanged fixture source after the change. This task
runs no validation/builds concurrently with either timed series. A shared
host is not a quiescent laboratory; small effects remain qualified.

Each Vitest export retains means, medians, operation counts and remaining
statistics. Its per-iteration `samples` array is empty by exporter design;
those individual observations are not available. The five independent
two-second means per fixture are converted from milliseconds to rounded
`ns/op` for benchstat. Do not treat the within-window operation counts as
independent benchstat samples. All 70 window summaries survive.

Median of the five window means:

| Fixture | Before ns/op | After ns/op | Change |
|---|---:|---:|---:|
| ASCII 256 bytes / 0 findings | 34,368 | 34,843 | +1.38%, not significant |
| ASCII 256 bytes / 1 finding | 33,705 | 34,348 | +1.91%, not significant |
| ASCII 65,536 bytes / 0 findings | 9,050,615 | 8,952,963 | -1.08%, not significant |
| ASCII 65,536 bytes / 1 finding | 9,230,745 | 8,984,467 | -2.67%, not significant |
| ASCII 65,536 bytes / 256 findings | 25,721,731 | 9,471,950 | -63.18%, about 2.7x faster |
| ASCII 65,536 bytes / 960 findings | 68,665,432 | 9,995,037 | -85.44%, about 6.9x faster |
| Unicode 40,960 bytes / 512 findings | 10,342,432 | 2,582,015 | -75.03%, about 4.0x faster |

All three many-finding comparisons have `p=0.008`, `n=5`. Small-case
slowdowns are retained, not silently dropped. Benchstat's warning that
five samples do not establish its 95% confidence intervals is retained.

JavaScript does not expose Go-style B/op/allocs/op here. They are not
fabricated as zero, and allocation savings/unchanged allocation counts,
peak heap or total-process memory bounds are not claimed. The code adds
no heap index/cache, but that is not a measured allocation result.

The JSONL evidence bundles the original Vitest export objects, normalizing
only their workspace file paths. Translated time rows and ASCII-normalized
benchstat output retain every fixture/sample. Evidence lives in
`docs/issues/security-r5-preview-highlights-2026-10-19/`.

Run from `web/` without relying on a stale `pnpm run` installation:

```sh
for sample in 1 2 3 4 5; do
  ./node_modules/.bin/vitest bench tests/unit/markdown.bench.ts \
    --run --environment node --outputJson "before-${sample}.json"
done
```

Repeat unchanged into `after-*.json`. For each exported benchmark, the
benchstat-compatible row is
`BenchmarkRawHighlightCPU/<name> <sampleCount> <round(mean*1000000)> ns/op`.
This benchmark is opt-in through Vitest; the Go `make bench` gate does not
automatically execute TypeScript benchmarks.

## Actual Browser Consumer

The new Playwright witness uses the built digest-pinned Marketplace page,
real resource API normalization, the composable and `ResourceDetailView`,
not an isolated helper or differently labeled mock function. Only API
responses use local test fixtures, following the existing browser suite.
The public API's admission policy is not raised or bypassed.

After catalog hydration, follow its real digest-pinned link. The fixture
has 65,627 UTF-8 bytes, 960 ASCII findings, one complete emoji finding and
one invalid interior-byte finding: all 961 valid marks must appear, with
the split-byte finding absent. Assert complete original visible text,
every marked text, first/last IDs, accessible label/tabindex, inert
script/image-like content, no event-handler elements/dialogs and no
remote requests. Both original and candidate built applications pass.

Each timing sample begins with a fresh navigation/composable, so it
cannot reuse a warm `highlightedContent` computed result. Timing starts
inside the browser at the actual Raw button click and ends after two
animation frames. It includes Vue computation, DOM/layout/frames and
browser scheduling, but excludes page/API/navigation setup and Playwright
protocol latency. The witness asserts no requests during each timed
interval. No brittle latency threshold is added.

Chrome 155.0.8059.80, desktop viewport; one browser run per side, five fresh
component renders in each:

| Before ms | After ms |
|---:|---:|
| 395.1 | 41.1 |
| 243.6 | 32.6 |
| 244.0 | 32.9 |
| 244.3 | 31.6 |
| 251.4 | 30.1 |

Observed medians are 244.3 ms and 32.6 ms. These are descriptive browser
observations, not five independent browser launches, an isolated lookup
speedup, a full page-load benchmark or an SLO guarantee. Browser/JIT/cache
state and frame scheduling remain relevant.

The candidate additionally passes existing hostile primary-Markdown and
lazy secondary Markdown/JSON preview browser controls. A separate
390x844 viewport run passes the same complete many-finding witness; its
timings are retained but not presented as a paired mobile speedup or a
native mobile/Safari test.

## Preservation And Negative Controls

The permanent 37-case helper suite compares independently authored
whole-output expectations for every valid mixed-width UTF-8 endpoint,
UTF-16 slicing, lone surrogates, undefined/negative/fractional/nonfinite/
interior/out-of-bounds/reversed/empty endpoints, and a valid finding beside
each rejected range. It also pins stable ties, strict overlaps versus
adjacency, deduplicated label order, hostile label escaping, safe index
fallback, sanitized-source offsets, frozen inputs and empty/plain output.
It and the existing Markdown suite pass before and after (48 cases).

Negative controls:

- Returning the next boundary for a missing exact byte breaks three
  interior-endpoint preservation cases.
- Merging adjacent ranges breaks the independently authored overlap/
  adjacency/label output.
- Truncating valid findings to one makes the actual built-browser witness
  fail with one mark instead of 961. It cannot pass vacuously without
  exercising the many-finding rendering path.

All mutations are removed. The final diff changes only the shared boundary
lookup, its tests/benchmarks and design/evidence documentation.

## Validation

Frontend preparation, ESLint, Nuxt typecheck, API type drift and all 249
unit tests pass, as does the production build. ESLint retains two existing
`vue/no-v-html` warnings on unchanged `ResourceDetailView`; local typecheck
emits a Vue Router Volar-plugin notice while exiting successfully.
Three desktop browser cases and the 390px many-finding witness pass.
Focused local browser runs import the stock Playwright configuration and
start only its mock and main-app servers, using the freshly built app.
They do not stand in for the other origins/sites/proxy in the full suite.

Scoped API/client short-mode tests, vet, zero-issue Go CI lint/build, six
explicit CLI regressions and focused API inspection read/refusal races
pass. Strict specs pass: 60 items, 1,038 scenarios, 1,660 annotations and
archive audit. Combined E2E/configuration compatibility passes with
`HARNESS_COMPAT_TARGET=all`: 2,927 invocations, 113/113 command requirements,
no listed gaps and all three required refusal witnesses executed/passed.
Compatibility shell checks pass. Vendor-native mode is explicitly
unavailable for this local configuration-only run.
These are scoped local gates, not a full root-suite, vendor-native,
released-client/container or full frontend multi-origin E2E claim.

Commands include `make validate` with
`TEST_PACKAGES='./pkg/marketplace/api ./internal/marketplace'` and
`TEST_FLAGS='-count=1'`, `make spec-check` and `make validate-e2e`.
Explicit CLI cases are `TestServeRequiresConfig`,
`TestServeRejectsUnsupportedRole`,
`TestServeRejectsDigestOnlyRevisionRevocation`,
`TestServeReindexRequiresConfig`, `TestNativeMCPStatusDestinations` and
`TestReadOnlyCommandsPreserveWarmExactCacheBytesAndMetadata`.
Focused API races cover `TestInspectionRoutesSupportEveryMarketplaceKind`,
`TestExactSkillInspectionIsImmutableAndReadableWhenRevoked`,
`TestResourceInspectionRetainsOversizeManifestStatus` and
`TestResourceInspectionContentReadsStopAtExistingCeiling`.

## Host Adversarial Review

Host review uses `open-code-review-delegate`: OCR supplies deterministic
file selection and rule groups, not an external model endpoint or a
delegated reviewer. The union of `origin/main..HEAD` and workspace previews
contains 15 intended files: five default-reviewable and ten default-excluded
files. All 15 are explicitly reviewed, none skipped; coverage is 100%.
No actionable findings remain.

Coverage includes the production lookup, permanent benchmark, helper and
browser tests; design, execution update and this report; both complete
five-run JSONL exports, both converted time files and benchstat; all three
browser timing records. Raw export fields and every converted value are
checked, not just headline medians. Review checks exact boundary/termination/
UTF-16/empty/invalid semantics, XSS and label/ID preservation, non-vacuous
actual-consumer witnesses, input immutability, API admission, no hidden
policy/cache/threshold change, all small controls and measurement/memory
limitations, scoped gate claims and explicit partial closure.

## Remaining Work

R38 remains partial: this removes the confirmed repeated boundary search
through the real browser consumer. Boundary construction still encodes
characters; sanitization, line/column fallback, Markdown processing,
reactive rendering and DOM size retain separate costs. No global render,
read, graph, memory or processing bound follows. #434 stays open, the
frozen artifacts remain unchanged and D1-D6 remain undecided.
