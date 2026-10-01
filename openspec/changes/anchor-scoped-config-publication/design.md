## Context

Existing filesystem work predates main's captured LayerIntent and workspace split. This slice ports shared filesystem primitives independently and integrates the current consumer APIs instead of resurrecting the removed runtime loader. The normative config-mutation contract continues to require missing-root creation and dangling read behavior.

## Decisions

1. Capture managed destination roots and opened objects using the shared Unix boundary. Read bytes, generation and mode from the same inspected descriptor; keep the existing dependency-file size policy.
2. Keep the logical config layer and its acquisition-lock location distinct from an approved content target. Preserve captured provenance and match staged overlays using their requested layer destination where necessary.
3. Preserve user dotfile links only for current-user-owned single-link regular targets. Repository and personal-local links remain confined to the captured root. Refuse unsafe ancestors and cross-layer object aliases before publication.
4. Keep descriptors through live transaction publication; recovery rebinds and validates journal state. Stable error identities survive wrapping. Preserve no-op output and existing modes.
5. Review consumers as well as primitives: direct edits, staged LayerEditor mutation, current deployed-lock readers, both migrations, lock reconciliation, check/read-only modes and purge-config.

## Risks and verification

The old positional classifier and raw-relation alias preflight require adversarial review on current main. Keep organization trust separate, preserve macOS canonical root aliases, and do not broaden personal-local authority to user authority merely because the repository is HOME. Tests must prove successful edits and refusal preservation, not incidental parse failure. Verify typed errors, external bytes/modes/mtimes/identity, links and staging residue. Benchmark snapshot and unchanged-commit disk paths before and after; validation cost is reported, not removed.

## Non-goals

Harness output roots, primary/support reads, portable manifests, native aggregates, backup restore and durable ownership publication are not closed by this slice. No global processing ceilings or unrecorded compatibility-policy changes are included.
