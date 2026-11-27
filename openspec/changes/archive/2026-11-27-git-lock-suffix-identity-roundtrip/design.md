# Design

## Context

`Normalize` canonicalizes each Git URL in a copied lock document, and `Encode`
serializes that normalized copy. A valid registry identity can itself end in
`.git` after removing one suffix from a repeated-suffix input. Treating that
canonical identity as a new raw URL on a later pass removes another suffix.
Consumers that re-canonicalize a normalized URL can make the same mistake.

## Goals / Non-Goals

- Preserve the canonical identity and existing v1/v2 wire schemas through
  repeated normalization and serialization.
- Keep raw `Document` literals as raw URL input and retain ordinary lock bytes.
- Carry the same identity through acquisition, targeted upgrade, fetch fallback,
  and cache reachability.
- Do not change strict target-profile identity, add a lock field/version, or
  change ordinary Git URL canonicalization.

## Decisions

### Keep the exceptional identity as private in-memory state

Add a private suffix identity marker to `GitRegistry`. Set it only when the
canonical URL ends in `.git`. `FetchURL` adds one suffix only while `URL` still
equals that marker; otherwise it returns the current URL unchanged. `Normalize`
validates `FetchURL()` on every pass, then resets the canonical URL and marker
from the validated result. A changed URL therefore cannot reuse a stale marker.

`NewGitRegistry(canonical, ref, commit)` is the constructor for acquisition
results that already hold canonical URLs. It uses the same setter as
normalization. Raw struct literals remain raw input. `FetchURL()` exposes the
wire spelling to encoding and to internal consumers that need a transport
fallback.

### Encode only a normalized copy

`Encode` keeps its input immutable: it normalizes the cloned document, changes
the clone's registry URLs to their `FetchURL()` values, then serializes the
existing YAML fields. Repeated-suffix identities therefore retain their
distinguishing spelling without adding a field or changing the API version.
Ordinary URLs continue to serialize exactly as before.

### Preserve identity at lock lifecycle boundaries

Use `NewGitRegistry` in Git and external-plugin acquisition. Keep canonical URLs
for lock matching, cache keys, and targeted-upgrade selectors. The selector
constructor passes the validated canonical URL; `SelectsGit` compares it
directly with the canonical requirement URL, preserving the sanitized identity
used by notices and deduplication. Configured fetch maps retain authored URLs
only as transport spellings. When a mapping is absent, use the lock record's
wire spelling rather than its shortened canonical URL.

Garbage collection normalizes each lock once and derives source, object, and
snapshot reachability from those normalized registry URLs. Value copies retain
the private marker; any new reconstruction from canonical fields must use
`NewGitRegistry`.

## Risks / Trade-offs

- **A changed URL could be accepted under stale identity state** -> `FetchURL`
  uses the marker only on exact equality, and normalization validates and
  resets the marker every time.
- **A consumer could strip the suffix again** -> compare already-canonical
  selector URLs directly, let GC consume one normalized document, and use the
  preserved wire spelling only for transport fallback.
- **The marker could affect comparisons** -> keep identity and deduplication
  keyed by canonical URL/ref; ensure equal identities are constructed and
  copied consistently, and retain the ordinary v1/v2 YAML goldens.

## Migration Plan

No data migration is required. The wire schema and API versions remain
unchanged. Existing ordinary lock bytes remain canonical and byte-stable;
repeated-suffix URLs round-trip using the corresponding existing `url` field.
