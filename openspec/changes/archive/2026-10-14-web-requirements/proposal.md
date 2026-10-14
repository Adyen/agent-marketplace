## Why

The Marketplace API now returns a resource's declared prerequisites and its dependents, but the resource page does not show them. Browsing users cannot see what a resource needs, whether each prerequisite is available in this Marketplace, or what depends on it.

## What Changes

- Add a "Requires" list to the latest resource page with a resolved, external, or unresolved badge per prerequisite, linking resolved entries to their resource pages.
- Add a "Required by" list linking each dependent, with explicit empty and unknown states.
- Show neither section when the response has no prerequisite data, including revision-by-digest pages.
- Add a conditional install note that names the prerequisite count and the `requires.expand` setting, without changing the copied command.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-web-ui`: present resource prerequisites, dependents, and the conditional install note.

## Impact

web/app resource detail component, presentation helpers, and component tests; web UI documentation. No API, OpenAPI schema, server, ingest, search, CLI, or TUI change.
