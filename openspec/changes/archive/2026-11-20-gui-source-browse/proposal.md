## Why

Selecting a source opens configuration instead of showing what that source offers. Marketplace browse is also limited to the initial results, so a user can miss resources unless they already know what to search for.

## What Changes

- Browse one source in Resources and clear the filter without opening Settings. Manage source retains the focused Settings reveal.
- Load the complete Marketplace browse inventory progressively, separate from search. Preserve useful results and report an incomplete catalog if a later page fails.
- Match resources to source identities, not display labels. Preserve existing settings, grouping and resource action semantics.

## Capabilities

### Modified Capabilities

- `gui-shell`: browse by source and progressively browse Marketplace catalogs.
- `gui-local-api`: associate Marketplace rows with the configured endpoint safely.

## Impact

Desktop workspace, local API discovery and source association. Reuse the shared Marketplace pagination and cache implementation after it lands on the delivery base. No consumer configuration schema, Marketplace server, TUI behavior or resource mutation changes.

## Release note

Select a source to browse its resources in place, or choose Manage source to open its Settings entry. Marketplace catalogs load in the background as you browse, without needing a search.
