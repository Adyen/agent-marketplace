## Why

The desktop Marketplace filter rail becomes taller than the viewport when many registries are shown. Its sticky positioning then prevents users from reaching the filters below the fold.

## What Changes

- Bound the sticky desktop filter rail to the available viewport height.
- Allow the filter rail to scroll vertically when its contents exceed that height.
- Preserve the existing non-scrollable presentation when all filters fit.

## Capabilities

### New Capabilities

- `marketplace-browser`: Browser behavior for discovering Marketplace resources.

### Modified Capabilities

None.

## Impact

The Marketplace web catalog layout and its Playwright responsive-layout coverage are affected. APIs, dependencies, and wire formats are unchanged.
