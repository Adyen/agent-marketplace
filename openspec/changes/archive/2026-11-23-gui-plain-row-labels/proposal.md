## Why

Resource rows show raw identifiers: the kind column prints `mcpserver`, scope tags print `organization`, and harness overrides print `harnesses: claude`. These read as internal names rather than as the words the rest of the app uses.

## What Changes

- The kind column shows singular display names: Skill, Command, Subagent, Hook, MCP server, Collection, Rule.
- Scope tags read Org, Project, and Local.
- The harness override tag reads "Only <harnesses>" in an outlined style, distinct from the filled scope tags.
- Facet and section titles keep their plural names.

## Capabilities

### Modified Capabilities

- `gui-shell`: resource row kind, scope, and harness override labels.

## Impact

Desktop GUI resource rows (`gui/src/ResourceList.vue`, `gui/src/grouping.ts`, `gui/src/style.css`) and their tests. The row status text is not changed by this change. No configuration, local API, Marketplace, TUI, or resource behavior changes.

## Release note

Resource rows in the desktop app show readable kind names, Org, Project, and Local scope tags, and an "Only" tag for harness overrides.
