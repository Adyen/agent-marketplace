## Why

Showing six collapsed registry options can place the **Show all** action just below the visible page area on common desktop layouts. Users should see the expansion action without first scrolling the filter rail.

## What Changes

- Show five registry options before the expansion action.
- Continue keeping selected registries visible even when they are outside the initial five.
- Preserve the existing expanded list behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-browser`: Define the collapsed registry-filter limit that keeps the expansion action visible earlier.

## Impact

The catalog filter component and its component tests are affected. APIs and persisted data are unchanged.
