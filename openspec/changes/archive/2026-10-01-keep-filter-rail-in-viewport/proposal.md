## Why

The desktop filter rail is bounded from its sticky offset even before it reaches that offset. Near the top of the catalog, this lets the rail extend behind the bottom of the viewport and hides controls while the resource list remains stationary.

## What Changes

- Size the desktop filter rail from its current viewport position.
- Recalculate its available height as page scrolling or viewport resizing changes that position.
- Add browser coverage for the rail before it reaches its sticky offset.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-browser`: Clarify that the desktop filter rail remains viewport-bounded before and after it becomes sticky.

## Impact

The Marketplace catalog page and responsive Playwright coverage are affected. APIs, persisted data, and dependencies are unchanged.
