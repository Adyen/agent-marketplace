# R5 Lazy Exclusion Writes

## Scope

This independent R53/F136 slice starts from main `656ef4db`, after the
filter-before-sort change, and integrates QA-only main `736e4779`.
It does not close R53, R5, or the frozen ledger.
No new selection policy, input ceiling, timeout, cache, or index is proposed.

Both v4 request interpreters use a local `map[string]bool` for exclusions.
They currently insert `false` for every explicit include, then test the map
with a plain boolean lookup. A missing entry already means the same thing
as an entry whose value is false.

## Smallest Change

On an explicit include with a non-nil request, write false only when the
local exclusion map is non-empty. Apply the same guard to `rebuildRequests`
and `effectiveV4Requests`.

An empty map cannot contain a true marker, so its boolean lookup is already
false. Healthy selections no longer grow this map at all. Once an exclusion
has populated it, retain the existing assignment behavior: re-inclusion
clears an earlier marker, and later exclusions can set it again. This does
not promise a true-only set or sparse storage after the first exclusion.
The selected-request map and ordered decision stream are unchanged.

Deleting markers was measured and rejected. It saves allocations for
healthy selections, but repeatedly excluding and re-including one locator
regresses the churn controls. Retaining false markers after actual
exclusions avoids repeated map deletion/reinsertion. Keep that rejected
paired dataset separately from the final guard variant.

Pre-sizing the exclusion map was rejected: it would still retain unnecessary
false entries and can over-reserve for repeated selections. A new set type,
shared interpreter, or persistent selector state is unnecessary.

## Preservation Contract

- Required protection and replacement by optional/required requests remain
  governed by the same ordered decisions.
- Exact, kind, registry, registry-kind, and global exclusions keep their
  existing matching and precedence.
- Repeated include/exclude/re-include sequences retain the final request,
  metadata, harness settings, provenance, and lexical output order.
- A nil request does not clear a marker. Include-only fallbacks remain
  rebuilding-only and do not reset an existing exclusion.
- Rebuilding retains its nil/empty behavior and authored decisions;
  effective interpretation still returns a non-nil empty slice.
- No input groups, authored files, lock evidence, diagnostics, scopes,
  acquisition, or publication behavior changes.

The boolean-map membership itself is not exposed: neither local map uses
the comma-ok form or enumerates false entries.

## Measurements

Retain the existing rebuild/public-parse benchmarks and add effective
interpretation/public-runtime-adapter coverage for the same healthy,
half-excluded, and fully-excluded fixtures at 1, 256, and 1,024 skills,
each retaining an unrelated command. Fixture construction is outside timing.

Add a narrow CPU-only churn control: 64 or 1,024 exclusion/re-inclusion
rounds for one locator in an expanded decision stream. This is not a
measurement of include-tree parsing, disk reads, or network work.

Build original and optimized test binaries from identical benchmark/test
definitions, replacing only the original production source through a
physical-path Go overlay. Verify distinct compiled code for both changed
functions. Run six alternating before/after trials at one second per case,
checking source/binary hashes and all 36 case identities. Keep the initial
three-sample probe separate and do not claim significance from it.

Report time, bytes, and allocations for each path and density, including
healthy, complete-exclusion, small-input, and churn controls. Do not pool
different baselines or infer a universal improvement from a geomean.

## Verification And Delivery

Run the per-locator preservation oracles with 1,000 generated cases on
both binaries and extend repeated re-inclusion cases. Mutation controls
must reject failing to clear a marker and clearing the wrong marker.
Check races, selected config/consumer validation, strict specs, terminal
QA, and witnessed E2E. This is behavior-preserving and needs a justified
`Spec-Impact: none` trailer rather than a new behavior specification.

Review every own file with `open-code-review-delegate`, including excluded
tests, raw evidence, and documentation, before each push and handoff.
Follow CodeRabbit, Gemini, and CI feedback. Leave the clean PR open without
enabling auto-merge or adding it to the merge queue.
