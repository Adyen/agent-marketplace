# Proposal

## Why

The Resources workspace can browse by source but cannot combine that scope with kind or status facets, and the selected source has no contextual summary or direct actions. Adding these controls makes browsing and safe source management available where the source is already in focus without changing configuration authority.

## What Changes

- Add independently clearable kind and status facets that intersect with source and search.
- Add a distinct All resources reset that clears source and facets while preserving search.
- Show projected source metadata, attributable problem counts, and safe copy/open actions in the Resources workspace.
- Route eligible source removal through the existing Settings confirmation and operation.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `gui-shell`: define stackable workspace facets and contextual source metadata/actions, including confirmation-preserving removal.

## Impact

Affected areas are the Vue workspace and sidebar, source header presentation, shared clipboard behavior, Settings source reveal flow, GUI tests, and GUI design documentation. The local API schema and persisted configuration remain unchanged.

## Release note

Resources can now be narrowed by source, kind, and status together, with contextual source details and safe actions in the workspace.
