# Collection Expansion Work Limit (#434: R11)

Decision: D2, approved in the [#434 memo](https://github.com/Adyen/agent-marketplace/issues/434#issuecomment-6519245552). The memo's option A was rejected and its approved fallback, a work limit aligned with lock bootstrap, was implemented.

## Why the memo option was not used

Nested expansion depends on its parent:

- the via label of the path (`collection:a > collection:b`);
- harnesses narrowed by the containing collection;
- the source, required flag and precedence inherited through `nestedCollectionArtifact`;
- branch-local ancestry for cycle errors.

Diagnostics are also appended once per path. Memoizing one expansion per collection would therefore change diagnostic multiplicity and could not be proven identical.

## Change

One resolve shares a budget of `lockfile.MaxMarketplaceLockResources` (13,981) units. Each expanded collection costs one unit, plus one per member and one per include. On overrun, the resolver appends one hard error wrapping `ErrCollectionExpansionLimit` and stops expanding. Sync returns before materialization whenever any hard resolve error exists, so nothing is written or pruned. The lock-bootstrap constant moved to `internal/lockfile` as the single authority.

## Evidence

`BenchmarkResolveCollectionDiamond`: Apple M4 Max, macOS, Go 1.27.3, 6 samples per side. Raw data: [collection-expansion-limit-2026-10-30](collection-expansion-limit-2026-10-30/).

| Diamond | Before | After |
| --- | ---: | ---: |
| Depth 8 (expands) | 1.297 ms | 1.302 ms (~, p=1.0) |
| Depth 16 | 449.6 ms | 10.5 ms (refused) |

Before the change, each further level doubled the cost; the [D2 verification](https://github.com/Adyen/agent-marketplace/issues/434#issuecomment-6274805199) measured about 2 minutes at depth 24.
