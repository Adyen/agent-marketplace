## Context

The latest resource detail response carries optional `requires` (ResourceRequirement: kind, name, registry, path, resolved) and `required_by` (ResourceRelation: registry, kind, name). Revision-by-digest responses never carry them. ResourceDetailView.vue renders both the latest and revision pages; generated types live in web/app/generated/api.d.ts.

## Goals / Non-Goals

Goals are the delta-spec behaviors. Non-goals: API, schema, server, ingest, search, CLI, or TUI changes, and any change to the copied install command.

## Decisions

- Classify badges in one pure presentation helper (resolved, external, unresolved) so tests cover it directly.
- Build links with the existing latest-resource route helper from the `resolved` or dependent relation, never from hint text.
- Render hints with text interpolation only.
- Gate both sections on the presence of `requires`; distinguish absent `required_by` (unknown) from an empty array (none).
- Add the install note inside the existing installable branch only, so the unavailable notice is untouched.

## Risks / Trade-offs

An `external` badge cannot tell a registry of another Marketplace from a hidden registry of this one, because the API makes hidden targets indistinguishable from missing ones. The badge therefore means "unresolved here and hinted elsewhere".

## Migration Plan

None.

## Open Questions

None.
