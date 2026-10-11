## Why

After the introduction scrolls away, the desktop search, resource count, and active-filter controls should remain available while users browse a long resource list. This should align with the sticky filter rail and preserve document scrolling for resource rows.

## What Changes

- Group the desktop catalog controls above the resource rows.
- Keep that group sticky below the primary navigation at the same offset as the filter rail.
- Continue scrolling resource rows through normal page scrolling beneath the sticky controls.
- Preserve mobile behavior and expanded filter scrolling.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-browser`: Add sticky desktop catalog controls while resource rows continue page scrolling.

## Impact

The Marketplace catalog template, desktop CSS, responsive browser tests, and Marketplace browser specification are affected. APIs and persisted data are unchanged.
