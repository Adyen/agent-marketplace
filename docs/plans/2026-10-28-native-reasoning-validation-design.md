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
- Consult a family pin's capabilities without rewriting the native selector.
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
