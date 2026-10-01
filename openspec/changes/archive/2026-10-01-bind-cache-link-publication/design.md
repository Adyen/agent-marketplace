## Context

Static root/ancestor admission does not prevent later path-based publication or cleanup from resolving a different directory. Artifact files and skill directories share the generic cache-backed link publisher and retain distinct ownership decisions.

## Decisions

1. Extend the existing directory capture path to return owned handles; directory-preparation wrappers keep their existing behavior and close the handle.
2. Stage and clean attempt-owned entries through the captured directory. Recheck the logical parent against the captured identity and the leaf against the generation observed during ownership validation immediately before rename.
3. Use the existing supported Go standard-library root operations for descriptor-contained native directory-tree migration, rather than importing a new recursive-work ceiling.
4. Keep user-owned skips and repeat-sync no-ops outside mutation. Preserve existing sensitive data and modes on pre-mutation refusal. Post-mutation persistence failure is not described as rollback.
5. Verification uses inert resources and deterministic publication barriers through materialize.Run. No fixture resource executes. No complete hostile same-user namespace exclusion or full filesystem consumer closure is claimed.

## Verification

The actual consumer replacement-parent regression fails before implementation because staging survives in the renamed directory. Add leaf replacement, managed-link update, no-op, manifest-owned migration and alias controls. Cover macOS/Linux and existing built-CLI lifecycle/compatibility gates, repeated races, strict specs and measured disk-path cost. Review every changed source, test and spec artifact.
