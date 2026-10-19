## Context

Preflight groups Codex destinations by a partition key built from the root and prefix strings, coalesces groups with more than one binding, and refuses managed paths of different partitions that are equal or nested. Roots are already symlink-resolved, so the only remaining aliases are spellings the filesystem itself folds: letter case and Unicode normalization.

## Decisions

- Compare managed paths of different partitions by a location key: the root replaced by the first spelling of the same existing directory (`os.SameFile`), then lower-cased. Equality or nesting of location keys refuses with `materialize.destination.overlap`.
- Lower-casing applies on every host, so a case-variant `CODEX_HOME` is refused on case-sensitive systems too. This follows the portable case-only collision rule for artifact paths.
- The partition key stays the exact spelling, so coalescing behaves as before. Disjoint partitions sharing a case-variant root remain usable because their location keys do not intersect.
- Refuse rather than coalesce. Coalescing aliased partitions would require canonical keys in plan execution, cached plan directories and ownership records, which all use the exact root spelling.

## Alternatives Considered

- Coalescing case-aliased partitions: rejected for the reason above.
- Only comparing existing roots with `os.SameFile`: misses roots that do not exist yet and differs between hosts.
