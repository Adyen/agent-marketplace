# R5 Raw Preview Highlight Lookups (R38)

## Scope

Start independently from main `f44101fa`, after R43 PR #751 merged.
Frozen R38/A128/F91 concerns repeated highlighting through the real browser
consumer, not only a helper benchmark. No ceiling, truncation, parser,
rendering policy or D1-D6 decision changes. The frozen artifacts remain
unchanged.

`ResourceDetailView` reads `useResourceInspectionPreview.highlightedContent`
when showing escaped raw inspection text. The shared highlighter builds
ordered UTF-8 boundaries for sanitized source, then linearly searches them
twice per finding. This repeats source-length work for each range.

## Alternatives And Choice

Retaining scans has the smallest code but repeats comparisons. A byte-index
map trades those scans for extra per-render retained entries. A sorted
endpoint sweep can be linear but adds range ordering/mapping machinery.

Prefer exact-match binary search over the existing monotonic array:
logarithmic lookup, no new index/cache/dependency and no changed boundary
construction. Measure before implementation; retain control cases where
lookup cost is not dominant. Do not incidentally optimize character
encoding, sanitization, line/column fallback or Vue caching.

## Preservation

Keep byte offsets relative to the existing sanitized string, exact UTF-8
boundary validation (never snap an interior byte), UTF-16 slicing, lone
surrogate behavior, invalid/empty/reversed range rejection, stable
start/end ordering, strict overlap grouping, deduplicated label order,
safe ID fallback, escaping and input immutability.

Add independently authored whole-output controls for mixed-width Unicode,
invalid endpoints, overlaps/ties/adjacency, unsafe labels and stripped
formatting. Exercise the real digest-pinned resource browser page with
many findings, including actual DOM marks, IDs, labels, selected text and
inert hostile content. Retain existing hostile primary/secondary previews.
Use negative mutations to prove the relevant controls are not vacuous.

## Measurement And Gates

Keep a permanent Vitest benchmark using the installed framework, not a
Go wrapper or new dependency. Capture five two-second samples for small,
zero/one-finding, wide ASCII/many-finding and multibyte cases before/after.
Include sanitization, encoding, range validation, grouping and escaping;
exclude fixture setup, disk/network and DOM. Preserve raw JSON and
benchstat-compatible time samples. JavaScript does not expose Go-style
B/op/allocs/op: do not invent them or claim memory bounds.

Separately witness the built browser consumer and record qualified
render timings without a brittle latency assertion. Report whether DOM,
frames, requests and warm computed values are included; do not present
helper timings as full browser latency. Run frontend unit/lint/type/API
checks and build, scoped root validation, specs, explicit API/CLI consumer
regressions and combined E2E with required witnesses. Complete host
OCR-delegation adversarial review before push. Leave #434 open and record
which R38 component is addressed.
