# Native Reasoning Validation

Approved scope: fix the shared resolver, not a Factory-only serializer.
The normative contract lives in the `validate-native-reasoning-effort`
OpenSpec change; this document records the implementation plan.

## Boundary

- Preserve the native model string during same-harness conversion.
- For models available in the destination catalog, accept native reasoning
  encodings and omit unsupported reasoning with the existing warning.
- Preserve unknown destination models and their native preferences, including
  identities known only to another harness target.
- Consult a resolved family identity's pin without rewriting its selector.
- Leave inherited selections and model-encoded reasoning paths unchanged.

## Implementation

Use the existing catalog encoding index in `modelpolicy.Resolve`. A native
model/effort pair already recognized by that index is valid; do not compare
native effort values only against canonical reasoning names. For aliases,
check the destination model's native encoding, following a family pin if
needed. Keep the existing diagnostic bridge and artifact writers unchanged.

The alternative Factory-only compiler guard would miss other consumers.
Rejecting every unknown native preference would break forward compatibility.

## Verification

1. Add a public Factory `Convert` regression and prove it fails before the fix.
2. Capture resolver benchmark baselines before modifying production code.
3. Fix the shared resolver, then add supported, unknown, other-catalog,
   family-pin, and native-encoding controls incrementally.
4. Run compilation-library validation, strict spec checks, and performance
   comparison with `benchstat`.
5. Archive the OpenSpec change, adversarially review every changed file using
   OCR delegate, and obtain publication approval before pushing.

## Native Family Follow-up

Recognize exact local family IDs for same-harness selections with a separate
effort field before the portable fallback. Do not expand cross-harness
inference or reinterpret undeclared native strings such as `sonnet`.

Use the existing pinned-capability guard once the family is known. Prove
Factory's shipped `anthropic/sonnet` through public `Convert`, remove the
synthetic concrete-model anchor from the catalog test, and retain supported,
omitted, opaque-selector, and cross-harness controls. Measure native and
existing resolver paths before and after the change, then validate, archive,
adversarially review, and publish the approved follow-up.

## Family Override Follow-up

Keep recursive target-native validation authoritative for declared family
overrides on separate-effort surfaces. The final override guard must not
discard supported reasoning merely because family IDs are absent from the
native-model index. Retain omission for unknown overrides and model-encoded
surfaces, and prove supported/unsupported family overrides through public
`Compile`. Benchmark the override paths before and after the guard change.
