## Why

The primary Marketplace header now has a fixed sticky height, so the desktop filter rail can use fixed CSS offsets. Runtime geometry measurement adds complexity that is no longer desired for this layout.

## What Changes

- Replace JavaScript rail measurement with fixed CSS `top` and `max-height` values derived from the 64px header and spacing.
- Define reachability for the rail once it reaches its sticky offset.
- Remove the pre-sticky dynamic-sizing behavior and its browser scenario.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-browser`: Scope the filter rail reachability contract to its fixed sticky position below the primary header.

## Impact

The Marketplace catalog page and responsive browser test are affected. APIs, dependencies, and persisted data are unchanged.
