## Why

Selecting or excluding a configured source resource by resource is slow. The existing whole-source feature must also retain current verification, scope, and v4 configuration rules after rebasing.

## What Changes

- Open a whole-source menu with `x` from a resource or a single-source status section.
- Select or deselect a Git or directory source with `g`, `p`, or `l`, and toggle its personal-local exclusion with `e`.
- Toggle optional organization Marketplace resources through user overrides.
- Preserve required resources, source identity, includes, unrelated selections, nested workspace roots, and serialized mutation handling.
- Create missing roots in v4 and reject unverified inventory mutations.

## Capabilities

### New Capabilities

- `tui-source-toggle`: whole-source menu, scope selection, and exclusion.

### Modified Capabilities

None.

## Impact

TUI input, rendering, configuration editors, resolver exclusion diagnostics, documentation, and real-terminal QA.
