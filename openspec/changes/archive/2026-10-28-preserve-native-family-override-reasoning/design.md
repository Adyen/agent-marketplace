## Approach

Keep the recursive resolver result for a declared destination family on a
surface with a separate effort field. The recursion already validates native
reasoning against the family or its pin. The final override guard continues
to omit effort for unknown override spellings and model-encoded surfaces.

Do not remove the guard or change native-model recognition, family pins,
cross-harness inference, diagnostics, or override authority.

## Verification

First fail a public `Compile` regression using a same-harness Factory override
to shipped `anthropic/sonnet` with supported `high` reasoning. Require the
authored override, reasoning, and body to survive without diagnostics.

Then cover unsupported family reasoning, omitted reasoning, an unknown
override, a normal native model, and a model-encoded family. Reject the old
resolver with an overlay and retain a control that rejects removal of the
unknown-override guard.

Capture five two-second benchmark samples before and after changing the
guard. Run affected validation, strict specs, the host E2E gate, and
adversarial OCR delegate review. Archive as the final commit before the
approved push and fresh automated reviews.
