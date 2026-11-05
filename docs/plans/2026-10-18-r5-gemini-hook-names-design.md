# R5 R42: Gemini Hook Names

## Scope

Start independently from main `ab3ab76c`, while PR #736 is queued.
Frozen R42/F106 concerns repeated Gemini hook-name collision scanning.
The frozen ledger/source index remain unchanged. No D1-D6 policy or new
ceiling, truncation, or hook-execution semantics are introduced.

## Design

`LoadHooks` sorts event keys and assigns a global unique resource name:
the historical canonical event stem plus authored/defaulted type, followed
by the first available numeric suffix starting at 2. Restarting that
search for every rule repeats all earlier collision probes.

Options:

- Retain the scans: simplest, but does not remove the measured repeated work.
- Add a second per-base cursor map: straightforward, with another index.
- Reuse the occupancy map with positive integer values: zero means free,
  and an occupied base remembers its next suffix. Reserve a newly generated
  name with cursor 2 so a later literal base cannot overwrite it.

Measure the third option. Global occupancy checks must remain because a
literal base can equal another base's generated suffix. Names are never
removed during an invocation, so previously occupied lower suffixes never
become available again.

## Preservation

Keep event/rule/group order, exact historical resource keys, unknown-event
diagnostics, nil results, authored command/type/prompt/timeout/matcher,
native provenance and group/rule extras. Keep malformed-input/path failures.
No global cache, filesystem writes or behavior/spec delta is needed.
Use a justified `Spec-Impact: none` trailer.

## Evidence

Before editing production, run authored collision cases and structured
Rapid comparisons against a pre-change restart-scan oracle. Benchmark
public `LoadHooks` for flat and native grouped inputs at small/large sizes,
plus the public bundle loader. Include JSON validation/decoding and IR
construction; exclude fixture creation and disk/network I/O.

Retain identical-fixture before/after samples and benchstat. Add a public
loader allocation regression that fails on the old repeated scan, then
measure small-case overhead rather than promising universal improvement.
Run focused package/SDK/catalog/materializer consumers, races, scoped
validation, specs and combined E2E, then review every intended file before
committing/pushing. Broader JSON reads and other frozen cases remain open.
