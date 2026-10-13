## Why

The same wordmark renders at different widths across the Landing, Docs, and Marketplace navigation: 217.41px, 200–208px depending on the Docs layout, and 154px respectively. Aligning only its height does not remove this visible width change.

## What Changes

- Give the visible logo artwork one shared fixed width across Landing, Docs, and Marketplace navigation.
- Preserve the wordmark aspect ratio instead of fixing height independently.
- Compare rendered image widths—not navigation wrapper widths—in shared-origin browser coverage.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-web-ui`: Require consistent navigation logo artwork width across all browser surfaces.

## Impact

Shared brand design tokens, VitePress navigation styling, Marketplace navigation styling, and shared-origin browser tests are affected. No assets, APIs, dependencies, or persistence behavior change.
