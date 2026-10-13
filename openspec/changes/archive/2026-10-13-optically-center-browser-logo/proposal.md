## Why

The logo image box is mathematically centered, but the supplied SVG's painted paths sit visibly above the navigation midpoint because of whitespace within its viewBox. The wordmark therefore looks vertically misaligned despite centered flex layout.

## What Changes

- Apply one shared downward optical offset to logo artwork on Landing, Docs, and Marketplace.
- Preserve the existing shared width, aspect ratio, wrappers, and navigation layout.
- Extend browser coverage to ensure every visible logo variant receives the same optical alignment.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-web-ui`: Require consistent optical vertical alignment for shared navigation branding.

## Impact

The shared brand token, VitePress logo styling, Marketplace logo styling, and shared-origin browser test are affected. No assets, APIs, dependencies, or persistence behavior change.
