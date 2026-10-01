## Why

Resource-row text and actions can paint through the sticky catalog controls because both use the same stacking level, and rows remain visible in the spacing below the primary navigation. Sticky controls must fully occlude scrolling content.

## What Changes

- Place desktop catalog controls above resource-row interactive content.
- Extend an opaque control backdrop through the gap below primary navigation.
- Add browser coverage for row occlusion in both the gap and control area.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-browser`: Require sticky catalog controls to visually occlude scrolling resource rows.

## Impact

Desktop catalog stacking CSS and responsive browser coverage are affected. APIs, dependencies, and persisted data are unchanged.
