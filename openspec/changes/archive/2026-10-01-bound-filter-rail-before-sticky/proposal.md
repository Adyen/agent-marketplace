## Why

The CSS-only rail height is calculated from its sticky offset, but the rail begins lower on the page while the introduction remains visible. A user who scrolls only the filters can therefore reach controls that remain below the viewport.

## What Changes

- Use a conservative CSS-only maximum height based on the rail's initial desktop position.
- Keep the filter rail within the viewport before the main page scrolls.
- Restore browser coverage for filter-only scrolling before sticky positioning engages.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-browser`: Add the pre-sticky viewport boundary for filter-only scrolling.

## Impact

The desktop catalog filter CSS and responsive browser coverage are affected. No JavaScript, API, dependency, or persisted-data changes are introduced.
