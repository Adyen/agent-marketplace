## Why

The accepted desktop preview uses more of the collapsed filter area and reduces the visual weight of the expanded scrollbar. The specification must record seven initial registries plus the thin scrollbar that appears on hover or keyboard focus.

## What Changes

- Show seven initial registry options while the filter is collapsed.
- Let the expanded filter scrollport fill the available desktop viewport area.
- Use a thin scrollbar whose thumb is transparent at rest and visible on hover or focus within the expanded filters.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-browser`: Update collapsed registry density and expanded-scrollbar presentation.

## Impact

The filter component constant, desktop CSS, component tests, responsive browser tests, and Marketplace browser specification are affected. APIs and persisted data are unchanged.
