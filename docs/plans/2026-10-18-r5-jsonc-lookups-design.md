# R5 JSONC Object Lookups (R43)

## Scope

Start independently from main `32259476`, while R47 PR #749 is queued.
Frozen R43/F108 asks for evidence of repeated JSONC merge/patch work
without changing handwritten syntax or fragment semantics.

No ceiling, truncation, parser acceptance, source policy or D1-D6
decision changes. The frozen ledger/source index remain unchanged.

## Investigation And Alternatives

`MergeWithExistingJSON` parses and decodes the destination before decisions,
then `patchObject` visits keys in source order and calls `Node.Member` for
each. `Member` linearly scans the same ordered members, making that
enumeration quadratic in object width.

Retaining scans is simplest but repeats comparisons. A per-object lookup
index avoids scans but adds retained maps, mutation invalidation and
potential read-time caching/concurrency changes. Direct source-order
member iteration avoids both the repeated scans and new cache state.

Measure the public merger before choosing an implementation. A small
read-only ordered iterator is the preferred alternative if the profiles
confirm lookup cost. Existing deletion snapshots, append ordering,
syntax ownership, equality and fail-closed parsing remain authoritative.
Do not change single-key lookup or mutation semantics incidentally.

## Evidence And Gates

Permanent CPU-only benchmarks include parse/decode, merge decisions,
patching and rendering for 1/32/256/1,024/4,096 keyed MCP entries,
unchanged and half-updated. Disk/network are excluded, not relabeled as
whole-SDK/CLI measurements. Capture five two-second samples per fixture
before and after without concurrent validation; retain raw data,
benchstat and small-case/allocation tradeoffs.

Preservation controls cover wide objects, original source order, escaped
keys, handwritten collisions/numeric spelling, edited/pruned managed
entries, comment association, sorted appends and byte-exact repeat merges.
Existing duplicate/malformed refusal and public SDK/native/CLI controls
must continue to pass. Add direct iteration controls if exposing an
iterator; exercise early termination and non-object values without
introducing shared mutable read state.

The baseline profile confirms repeated `Member` work. Use the standard
library's `iter.Seq2` to expose a read-only source-order walk and consume it
only after `patchObject` finishes deletions, before it appends new keys.
Value replacement remains supported during the walk; structural mutation
is explicitly outside the iterator's contract.

The first sequential default-CPU comparison is confounded by concurrent
host activity. Retain it, then compare interleaved precompiled binaries
with `-test.cpu 1`, alternating pair order and retaining five two-second
samples per fixture. This setting is benchmark-only, never a CLI budget
or concurrency-policy change.

Run scoped SDK/artifact/JSONC/catalog/materializer validation, explicit
CLI consumers, focused races, strict spec/archive checks, combined E2E
with required witnesses and host OCR-delegation adversarial review.
This is not a claim of global parser, graph, memory or acquisition bounds.
