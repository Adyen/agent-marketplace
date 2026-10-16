## Context

Disk-backed catalog loading already records the actual admitted primary file as `Entry.PrimarySourcePath`, including a native harness root prefix when required. `Entry.Path` is an absolute logical path, while the IR source file can remain bundle-relative. `catalog.SourcePath` currently prefers the IR value unconditionally. Both bundle-tree creation and captured preview content use that helper.

See proposal.md for the user-visible problem and specs/tui-navigation/spec.md for the behavior contract.

## Goals / Non-Goals

Use the existing catalog inspection helper as the single policy point. Keep compiler IR provenance and materialization paths unchanged. Do not add cwd-dependent searching or path-existence fallbacks.

## Decisions

Prefer a recorded admitted `PrimarySourcePath` in `catalog.SourcePath`. Without it, retain an absolute IR source reference for exact snapshots and existing synthetic callers. A relative IR reference must not displace the loader's resolved `Entry.Path`.

This uses the source authority already established during disk loading, avoids mutating compiler IR, and covers native layouts where the logical path omits a harness root. Changing only bundle-tree construction would leave preview capture broken. Always preferring `Entry.Path` would lose exact snapshot identity after display-path rebasing.

Tests use real disk discovery into TUI rows, with absolute and relative registry configurations, skill support files, a native source layout, consumer decoys, and post-discovery disappearance or unsafe payload changes. Existing Marketplace and exact snapshot controls remain decisive regression checks.

## Risks / Trade-offs

- Logical source paths can differ from admitted paths in native layouts. Use the admitted path rather than reconstructing a harness prefix in the TUI.
- Some catalogs have no admitted primary record. Preserve their absolute IR source path and resolved entry fallback, with focused coverage.
- Inspection remains a live local read rather than a whole-tree atomic snapshot. Preserve existing descriptor-bound payload checks and read bounds.

## Migration Plan

No configuration or lock migration is needed. The fix changes read-only inspection path selection and can be rolled back without rewriting consumer state.
