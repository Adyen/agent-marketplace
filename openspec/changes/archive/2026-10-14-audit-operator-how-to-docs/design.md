## Context

See `proposal.md` for scope and motivation. Current runtime, deployment assets and governing specifications supply the operator facts; prose-only edits must not change them.

## Goals / Non-Goals

Record each page's reader, quadrant and source checks in private audit evidence. Move exhaustive configuration fields, error-code catalogues and architecture out of task instructions into links. Do not expand outside-page edits beyond required links.

## Decisions

- Retain the ten page URLs unless the page audit demonstrates overlap or a quadrant mismatch that a merge, split or move resolves; any restructuring updates `site/.vitepress/config.mts` and inbound links.
- Pair commands with exact or labelled typical results, including silent success and health-endpoint responses.
- Verify practical commands with isolated fixtures: build with `make build`, run Go programs through `go run`, and sandbox HOME and organization configuration. Check Docker Compose, proxy and service-manager steps statically against `deploy/` and source when running them is impractical.
- Keep existing specifications unchanged and mark this change `skip_specs: true`. Documentation corrects its claims when current source or normative requirements contradict them.

## Risks / Trade-offs

- Operator output depends on storage backend, registries and policy. Label variable results rather than presenting fixture output as universal.
- Backup, reset and revocation steps are destructive or externally visible. Validate them against source and isolated fixtures without touching real deployments.
- A reference-only documentation test does not prove operator-guide correctness. Keep the per-page source audit and independent reviews alongside build and contract checks.

## Migration Plan

Publish the guides through the normal site build. No runtime migration or schema change is needed.
