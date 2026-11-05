# Strict Harness Target Policy

The accepted decision and examples are in
[ADR 0010](../adr/0010-require-supported-harness-target-ids.md):
**Your CLI must understand every tool named in the configuration.**

## Implementation

1. Record option 1 in #935 and #928, distinguishing the accepted decision from
   implementation still awaiting merge.
2. Create an OpenSpec change correcting the TUI save/resize requirements and
   explicitly describing unknown-target refusal.
3. Replace injected unknown-ID model cases with supported-ID selections while
   retaining selection, cursor, scope and compact-resize coverage.
4. Extend the known-ID terminal bindings and add visible unknown-ID refusal,
   guarded-write and unchanged-file checks with supported-input recovery.
5. Run focused tests, the complete terminal suite, scoped validation, spec
   checks and negative controls. Complete adversarial and OCR delegate review
   over every changed file before pushing.
6. Archive the OpenSpec change in the final implementation commit. Link the PR
   from both issues; do not mark the epic item complete until merge.

No runtime validation relaxation, warning-only compatibility, new harness
support, new dependency or product test hook is planned.
