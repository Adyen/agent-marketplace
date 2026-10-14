## Why

The top-level pages are the first pages every reader opens. `index.md`, `install.md`, `quick-start.md`, `glossary.md` and `terminal-basics.md` mix quadrants, carry stale examples, and the index still describes the user-journey page as giving steps for every interface. After the section audits, the site also needs one consistent term per concept and a sidebar that serves each reader.

## What Changes

- Audit the five top-level pages under `site/docs/` and the sidebar and nav order in `site/.vitepress/config.mts`.
- Give each page one Diátaxis quadrant and reader, and replace content from other quadrants with links.
- Verify commands, flags, configuration keys, paths, environment variables, diagnostics, versions and output against current source and specifications, running commands against a locally built binary in a sandboxed HOME where practical.
- Tighten prose and remove em dashes, marketing language and passive constructions.
- Sweep the whole site for one term per concept, aligned with the `openspec/config.yaml` glossary, and organize the index by reader.
- Restructure pages only when the audit justifies it, updating navigation and every inbound link.

## Capabilities

### New Capabilities

None. This change audits prose only.

### Modified Capabilities

None. Runtime behavior and normative requirements remain unchanged.

## Impact

The five top-level pages, the documentation sidebar, and spec-less change artifacts. Other pages may receive link and terminology fixes only. No runtime, test or normative-spec changes.
