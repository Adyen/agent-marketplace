## Context

Slices 1 to 3 give each ResolvedArtifact prerequisite provenance (RequiredBy), an Implied flag, a PreviewOnly flag, and give pipeline.ResolveResult an ImpliedPreview plus typed UnmetRequirementWarning reasons. The TUI snapshot already holds the resolved artifacts and the pipeline ResolveResult (inventoryResult). Collection handling parses Via (collectionNameFromVia, collectionRemoveTarget). Config writes go through the serialized dependency mutation path in bulk.go and actions.go.

## Goals / Non-Goals

Goals are the delta-spec behaviors. Non-goals: resolver, pipeline, CLI, configuration-schema, acquisition-lock, Marketplace, or web changes; Via parsing changes; confirmation dialogs; requires-edge writes.

## Decisions

- Derive per-row prerequisite state once while building rows from the snapshot: declared requirements from the catalog entry, satisfaction from selected kind/name, implied from Implied artifacts, preview from ImpliedPreview, and excluded/unmet from typed warnings attributed to the requiring resource. Rendering reads these precomputed fields so unchanged rows and render cost stay unchanged.
- Required by reads RequiredBy only; Via handling is untouched.
- Toggle off reuses the existing exclusion write that collection handling uses, through the serialized mutation seam, and applies it only when expansion is enabled and the resource is implied or still required. Required-policy and scope checks run first, as today.
- Toggle feedback compares the snapshot after the config write with the one before, limited to prerequisites reachable from the toggled resource, and posts footer toasts through the existing bounded notification path.
- Footer and detail text use existing terminal-escaping and redaction helpers.

## Risks / Trade-offs

In advisory mode almost every requiring row shows an unmet marker, which matches CLI status. Diffing snapshots must not misattribute changes caused by concurrent external edits; attribution is limited to RequiredBy chains rooted at the toggled resource.

## Migration Plan

None. The release-N expansion default stays false.

## Open Questions

None.
