## Context

See `proposal.md` for scope and motivation. Current runtime, governing specifications and the `openspec/config.yaml` glossary supply the facts; prose-only edits must not change them.

## Goals / Non-Goals

Record each page's reader, quadrant and walk record (commands run versus checked statically) in private audit evidence. Move content that belongs to another quadrant into links. Keep edits outside the five pages to link and terminology fixes, and leave the tutorial follow-up nits tracked separately untouched.

## Decisions

- Retain the five page URLs unless the audit demonstrates overlap or a quadrant mismatch that a merge, split or move resolves; any restructuring updates `site/.vitepress/config.mts` and inbound links.
- Pair every command with exact or labelled typical output, including silent success.
- Run documented commands with a binary from `make build` in a sandboxed HOME. Declare any command that cannot run locally, such as network installs or `sudo` steps.
- Use the `openspec/config.yaml` glossary term wherever the site names a glossary concept, and keep the public glossary's plain-language definitions consistent with it.
- Keep existing specifications unchanged and mark this change `skip_specs: true`. Documentation corrects its claims when current source or normative requirements contradict them.

## Risks / Trade-offs

- Release versions move. Name the current latest release and phrase version-specific claims so the next release does not make them wrong.
- A site-wide term sweep touches many pages. Limit outside-page edits to the term or link itself.
- A reference-only documentation test does not prove prose correctness. Keep the walk record and independent reviews alongside build and contract checks.

## Migration Plan

Publish the pages through the normal site build. No runtime migration or schema change is needed.
