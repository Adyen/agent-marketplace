## Context

See `proposal.md` for scope and motivation. Current runtime, deployment assets and governing specifications supply the facts; prose-only edits must not change them.

## Goals / Non-Goals

Record each page's reader, quadrant and walk record (steps run versus checked statically) in private audit evidence. Move choices, exhaustive options and architecture out of tutorial steps into links. Do not expand outside-page edits beyond required links, and leave the separate Kubernetes tutorial proposal untouched.

## Decisions

- Retain the five page URLs unless the audit demonstrates overlap or a quadrant mismatch that a merge, split or move resolves; any restructuring updates `site/.vitepress/config.mts` and inbound links.
- Pair every command with exact or labelled typical output, including silent success.
- Walk each tutorial with a binary from `make build` in a sandboxed HOME. The hosted-Marketplace tutorial walks against the local Compose Marketplace because no public Marketplace exists; admin-panel steps walk through the same local stack. Declare any step that cannot be walked.
- Keep existing specifications unchanged and mark this change `skip_specs: true`. Documentation corrects its claims when current source or normative requirements contradict them.

## Risks / Trade-offs

- Network-dependent steps (public GitHub registry, container images) can vary. Label variable output as typical rather than exact.
- A reference-only documentation test does not prove tutorial correctness. Keep the per-page walk record and independent reviews alongside build and contract checks.

## Migration Plan

Publish the tutorials through the normal site build. No runtime migration or schema change is needed.
