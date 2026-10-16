## Why

The Landing, Docs, and Marketplace surfaces need a more cohesive brand treatment and clearer navigation responsibilities. The shared logo is oversized, the Marketplace header repeats its current location, and its Docs link behaves like local navigation even though Marketplace deployments can run on arbitrary operator domains. The logo's green slash also lacks a restrained supporting motif in the surrounding interfaces.

## What Changes

- Reduce the shared Landing, Docs, and Marketplace logo width from 200px to 160px.
- Remove the redundant `Marketplace` text link from the Nuxt header while retaining the logo home link.
- Keep Landing Docs navigation local, but send the Marketplace header Docs link to `https://agent-marketplace.io/docs` in an isolated new tab with a visible external-link marker.
- Reuse a weight-800 `#00D16A` slash before Landing section eyebrows and the Marketplace instance title, and as the meaningful registry/resource delimiter in result rows.
- Keep Landing eyebrow labels in the primary text color and leave the hero secondary line plus Marketplace Filters and Resources headings unaccented.
- Replace deployment-tool-specific Landing copy with environment-neutral wording.

## Capabilities

### Modified Capabilities

- `marketplace-web-ui`: Shared logo sizing, Marketplace header navigation, and approved slash-brand placements.
- `marketplace-browser`: Branded resource-coordinate delimiter.
- `site-deployment-profiles`: Context-specific Landing and Marketplace documentation navigation.

## Impact

The change affects shared brand CSS, Landing presentation and copy, Nuxt header and catalog markup, and frontend regression coverage. It does not change API behavior, documentation inventory, resource coordinates, routes, filtering, or installation behavior.
