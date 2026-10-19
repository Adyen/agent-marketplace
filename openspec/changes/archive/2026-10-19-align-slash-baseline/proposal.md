## Why

The shared slash is an inline block whose default baseline is its bottom margin edge. Its full `0.8em` height therefore sits above the text baseline, making the mark appear to float high beside adjacent letters.

## What Changes

- Lower every shared slash by `0.08em` using `vertical-align`.
- Keep the offset font-relative across eyebrows, titles, coordinates, and detail titles.
- Add browser coverage for the shared baseline offset.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-web-ui`: Shared slash marks use a consistent optical baseline offset.

## Impact

The change affects shared brand CSS and browser regression coverage. Slash geometry, spacing, stroke, placement, and accessibility remain unchanged.
