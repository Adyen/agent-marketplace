## Why

New users need tutorials that work end to end on the first try. The five tutorials under `site/docs/tutorials/` mix learning steps with choices, reference detail and explanation, and some commands lack expected results or no longer match the current CLI and service.

## What Changes

- Audit all five tutorials under `site/docs/tutorials/` for first-time readers.
- Keep each page in the tutorial quadrant: one path, no choices, expected output at every step, with links to how-to, reference or explanation for other needs.
- Verify commands, flags, configuration keys, paths, environment variables, diagnostics and expected output against current source and specifications, and walk each tutorial against a locally built binary in a sandboxed HOME.
- Tighten prose and remove em dashes, marketing language and passive constructions.
- Restructure pages only when the audit justifies it, updating navigation and every inbound link.

## Capabilities

### New Capabilities

None. This change audits prose only.

### Modified Capabilities

None. Runtime behavior and normative requirements remain unchanged.

## Impact

The five public tutorials and spec-less change artifacts. No runtime, test or normative-spec changes. Other pages may receive required link fixes only.
