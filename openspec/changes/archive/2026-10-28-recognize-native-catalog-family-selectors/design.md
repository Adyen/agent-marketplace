## Approach

After native decoding and before portable inference, recognize an exact
source-catalog family ID only when source and target match and the surface
declares a separate effort field. The existing same-harness guard then checks
the family pin's native reasoning encodings without changing the authored
model string.

Do not adopt the broader source-catalog fallback for cross-harness inputs.
Do not add unqualified aliases: undeclared Factory `sonnet` remains opaque,
even though the exact `anthropic/sonnet` family ID is known.

## Verification

First fail a public Factory `Convert` case for shipped `anthropic/sonnet`
with unsupported `max`, requiring model/body preservation, true effort-key
absence, and one omission warning. Capture the benchmark baseline before
editing production code.

After the minimal guard, cover supported and omitted family reasoning,
undeclared selectors, and unchanged cross-harness behavior. Remove the
unrelated concrete Claude record from the synthetic family fixture and prove
that the old resolver fails the strengthened tests.

Compare five two-second benchmark samples for native and existing resolver
paths. Run affected validation and strict spec checks, archive the change,
and perform adversarial OCR delegate review before the approved publication.
