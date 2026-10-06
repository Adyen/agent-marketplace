## Context

Legacy marker-merge output aliases were read through a pathname and statted separately before being detached. This can import bytes from an unauthorized external file. Some contained aliases and legacy cache links are explicitly supported and must retain migration behavior.

## Decisions

1. Reuse the admitted parent capture and shared destination snapshot, requiring a regular, singly linked target owned by the current user before reading.
2. Authorize the captured effective target against repository/HOME scope, the trusted cache root, or the already-admitted machine output directory. Canonical scope identity handles filesystem aliases without treating lexical containment as sufficient.
3. Read the captured generation through one verified descriptor. The captured size bounds growth for that generation; this adds no arbitrary absolute byte ceiling or aggregate-work budget.
4. Apply alias admission to read-only checking as well as mutation. Keep existing detached regular-file merge behavior, handwritten data, permissions and no-op semantics.
5. The later atomic-write and generic merge phases remain outside this reader-only closure. No full marker-publication transaction or hostile same-user exclusion is claimed.

## Verification

Actual materialize.Run foreign-alias regression failed before the fix. Valid native MCP controls and contained alias migration cover Factory/Cursor/Windsurf at repository and machine scope, with original-target preservation and regular-file repeat no-ops. Include read-only refusal, shared snapshot shape/error controls, Linux/macOS parity, races, built-CLI compatibility, disk/no-network benchmark cost and complete adversarial review.
