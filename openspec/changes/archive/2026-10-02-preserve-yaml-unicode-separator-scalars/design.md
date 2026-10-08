## Context

YAML recognizes Unicode NEL, line separator and paragraph separator as line boundaries. The existing plain-scalar classifier only checks ASCII bytes, and the quoting choice does not escape Unicode boundaries. The shared formatter is used for native frontmatter keys and values.

## Decisions

1. Extend the existing scalar classifier and double-quoted escape choice for exactly the three YAML separators, using existing strings/strconv helpers. Keep ordinary quoting rules and all current comments intact.
2. Prove the authority boundary through public Windsurf Compile and an independently parsed native YAML map: description remains the exact input and does not produce auto_execution_mode.
3. Preserve ordinary golden output and explicit typed workflow authority. Fixtures are inert and no harness or generated command executes.
4. Scope is the confirmed Unicode scalar issue only; opaque metadata, native sidecar conversion, token meaning and other R4 policies remain separate.

## Verification

The actual native regression fails for all three separators on the prior code, with a parsed auto_execution_mode field. After correction, exact scalar data survives and the field is absent. Require supported target goldens, formatter/compiler packages, Linux/macOS, configuration/E2E gates, benchmark costs and complete adversarial review.
