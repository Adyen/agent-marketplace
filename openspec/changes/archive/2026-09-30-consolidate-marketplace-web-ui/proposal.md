## Why

Marketplace navigation, footer, and resource-detail refinements were developed through several small changes, leaving duplicated planning artifacts and fragmented UI contracts. Consolidating them keeps one reviewable source for the branch while preserving required OpenSpec traceability.

## What Changes

- Keep the Marketplace header visible while scrolling and retain independent web-owned chrome styling.
- Keep footer identity and typography consistent with Marketplace chrome.
- Refine resource identity, description disclosure, revision metadata, installation commands, and unavailable-state presentation.
- Consolidate the branch's three Marketplace UI OpenSpec changes into one capability and archive.

## Capabilities

### New Capabilities

- `marketplace-web-ui`: User-visible Marketplace page chrome and resource browsing interactions.

### Modified Capabilities

None.

## Impact

- Marketplace and landing navigation presentation.
- Marketplace footer and resource-detail components.
- Existing browser and component tests used for scenario traceability.
- Supersedes the branch-local `marketplace-web-navigation`, `marketplace-web-chrome`, and `marketplace-web-resources` specifications.
