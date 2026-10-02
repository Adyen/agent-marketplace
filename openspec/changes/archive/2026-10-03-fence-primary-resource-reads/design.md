## Context

Catalog LoadWithOptions sends os.DirFS to native ingesters. Detection-only paths construct the same unrestricted view. The shared boundary has captured directories, contained strict destinations, nonblocking regular/single-link admission and captured-size streaming/full identity checks; reuse it without importing old source-tree budgets.

## Decisions

1. Lazy SourceFS owns a captured Dir and its native Root. Directory opens use separately captured descriptors and close every owner. Payload Open/ReadFile returns immutable bytes only after strict descriptor-bound snapshot admission; Stat is metadata-only beneath Root.
2. Root aliases are authorized once by the caller. Primary file aliases may resolve within the source root; foreign targets and unsafe/special/multiply linked payloads refuse before bytes enter parsing. Native ingestion keeps its per-resource diagnostic/valid-sibling semantics.
3. Disk bundle/root probes use the same view, so detection cannot bypass payload admission. No eager whole-checkout scan, new processing ceiling or promise that every file in a mutable source is one atomic generation.
4. This foundation is followed by source-revision, other primary helpers and remaining R2 lanes; do not treat this catalog change as whole-R2 closure.

## Verification

Public catalog foreign primary alias was imported with zero diagnostics before the fix; corrected direct loading refuses it and retains the valid sibling. The native CLI witness then caught a real integration defect: treating the refusal as ordinary malformed syntax pruned the installed skill. SourceFS retains its first typed payload refusal under a mutex, Catalog carries it privately, and publication discovery now propagates it rather than downgrading it; direct catalog listing still retains siblings. Corrected native CLI preserves installed/external generation and fails closed, while ordinary syntax policy is unchanged. Contained authored primary alias and hard-link controls, bounded-child FIFO consumer and renamed-root/directory-info tests pass. Linux/macOS, count20 selected races, full affected short catalog/fsboundary/sourcerevision/resolver/pipeline, CLI, scoped validate/lint/build and strict specs pass. All14 implementation entries including native/CLI/benchmark/script/spec exclusions reviewed, no skips; final integrated/comment gates wait for full R2 delivery. Eight-primary disk/no-network load benchmark with logs discarded, 2s ×5 arm64: median612.434µs→3654.535µs (+496.72%), B/op116370→161100 (+38.44%), allocations912→1712 (+87.72%), p=.008 n5. Includes descriptor/stat/read validation and is a substantial validation cost, not a speedup; no95% interval with n5. Typed refusal retention changes error paths only, not these valid-load samples.
