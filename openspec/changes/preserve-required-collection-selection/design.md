## Context

Top-level collection selectors are propagated into registry entries before selection. The legacy automatic-selection predicate regards the propagated include as an authored explicit selection, narrowing a required registry.

## Goals / Non-Goals

- Preserve automatic required selection and cross-registry collection lookup.
- Keep explicit required subsets and caller configuration unchanged.
- Do not change optional/default selection, cross-kind collisions, collection expansion budgets, or configuration formats.

## Decisions

Skip collection-selector propagation only for required entries that have no authored explicit selection. Those entries already select their entire catalog, including any collection they provide. Add top-level collection requests to their occurrence lookup without changing the automatic-selection predicate, so absent or malformed requests still reach the existing diagnostic path. Other entries retain the existing propagation and missing-collection suppression path.

## Risks / Trade-offs

A required source may provide the requested collection or an unrelated collection. Controls cover both source locations and an explicitly authored subset so the guard does not broaden existing selection. CLI coverage verifies machine outputs survive repeated locked synchronization.
