## Context

DiscoverCollections is shared by catalog load/sub-bundles, direct Git revision building and Marketplace ingestion. Its independent path-based definition reads bypass primary catalog fencing.

## Decisions

1. Reuse SourceFS per discovery root. Directory admission and payload snapshots enforce captured containment, regular single-link/nonblocking and captured-size growth/state checks. Preserve lexical provenance rather than canonical target paths.
2. Genuine root/collections/definition absence remains optional; typed unsafe path refusal is never normalized to absence. Conditional source close errors are propagated, and results remain nil on refusal.
3. Sort native directory entries to retain existing deterministic collection order; preserve flat YAML and non-directory filtering and contained authored file aliases.
4. No arbitrary budget, policy change to selection/canonical collection IR or whole-tree snapshot claim. Remaining R2 consumers and final integration/GitHub review remain tracked separately.

## Verification

Public collection loader foreign definition was red/green: parent imported foreign members without error; captured loader refuses with ErrUnsafePath and preserves external bytes/identity/mode/mtime. Native CLI installed collection positive/control then external empty definition refuses rather than prune its native skill. Contained file alias/provenance, hard-link, dangling-not-absence and bounded child FIFO controls verify the shared path. Linux/macOS, count20 selected races, full affected short collection/catalog/source-revision/pipeline, native CLI, validate/lint/build and strict specs pass. All11 implementation entries including test/CLI/benchmark/script/spec exclusions reviewed with none skipped; scope/refusal/absence/cleanup/provenance/order and consumer reachability checked. Existing8collection native disk/no-network2s ×5 arm64 median256.197→1546.671µs(+503.70%),B/op84624→117237(+38.54%),alloc671→1054(+57.08%),p=.008 n5. Descriptor validation cost, not a speedup; n5 lacks95% interval.
