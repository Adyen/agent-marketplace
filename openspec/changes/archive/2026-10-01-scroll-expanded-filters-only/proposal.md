## Why

The filter rail should use the original natural page-scrolling behavior while collapsed. Independent filter scrolling is needed only after **Show all** renders a registry list taller than the viewport.

## What Changes

- Remove unconditional height and overflow constraints from the collapsed desktop filter rail.
- Keep the collapsed filters in normal page scrolling while preserving the sticky stop.
- Mark the existing expanded state in the rendered filter component.
- Apply bounded independent scrolling only while all registries are expanded.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-browser`: Replace always-scrollable desktop filter requirements with natural collapsed scrolling and expanded-only independent scrolling.

## Impact

The catalog filter markup, desktop CSS, browser fixtures, responsive tests, and Marketplace browser specification are affected. No API, dependency, or persisted-data formats change.
