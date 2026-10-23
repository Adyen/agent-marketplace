# R5: Request References During Selection Replay

## Scope

This independent R53/F136 performance slice is delivered on main `027056e3`,
including effective-request reuse. It changes the allocation cost of each
interpretation. The initial measurement matrix starts from `4dfa9dae`. R53, R5,
and the performance tracker remain partial. Frozen artifacts stay unchanged.

## Measured Cost And Change

Both selection interpreters use maps whose values are full request structs.
The baseline allocation profile attributes most effective-interpreter bytes
to inserting those structs. Growing from 64 to 1,024 requests adds roughly
970 allocations in each interpreter and in include-only fallback rebuilding.

Store pointers to already-existing input requests in the invocation-local
maps. The maps only read requests during replay. Dereference each retained
request into the output slice so returned structs keep value-copy semantics.
Take fallback addresses from the existing include slice by index, rather
than taking the address of a copied loop variable.

Preserve exact/global/kind/registry matching, ordered replacement and
re-inclusion, required protection, nil decisions, fallback distinctions,
metadata, provenance, lexical order, and nil/empty results. Keep public
interfaces and all parsing, validation, scope, acquisition, ownership,
and publication behavior. Add no persistent state, cache, index, dependency,
ceiling, or policy. Record `Spec-Impact: none` for this behavior-preserving
change rather than inventing an observable requirement.

## Verification

- Allocation-scaling regressions fail on all three original replay paths.
  Value-copy controls, existing per-locator oracles and real consumers prove
  preservation. Run generated oracles at 1,000 cases and focused races.
- Keep existing public-parser, rebuild, effective, adapter, and churn
  benchmarks. Add include-only fallback coverage. Setup is outside timing;
  these are CPU-only measurements without disk or network work.
- Compile original and changed sources with identical test/benchmark files.
  Retain separate baseline probes and six interleaved AB/BA pairs using
  two-second samples, source/binary identities, and all 39 case identities.
- After main integration, verify both replay function bodies are identical
  to the measured versions. Retain a separate six-pair, eight-case public
  Git adapter comparison against `027056e3`; do not pool baselines.
- Run selected validation, strict specs, combined E2E, and terminal QA.
- Screen outgoing content, use OCR plus host delegated adversarial review
  for every changed file, and address confirmed GitHub bot findings.
  Leave the resulting PR open with auto-merge disabled.
