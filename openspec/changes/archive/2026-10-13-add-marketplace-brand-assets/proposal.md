## Why

The Marketplace and documentation currently render a text-only product identity and do not publish the supplied product favicon. The two browser surfaces should use the same recognizable brand assets in both light and dark appearance modes.

## What Changes

- Display the supplied horizontal agent-marketplace logo in the primary navigation of the Marketplace and documentation site.
- Select the black or white logo variant to preserve contrast with the active appearance.
- Publish the supplied favicon from both frontend builds at their configured base paths.
- Preserve the existing accessible product name and home navigation behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-web-ui`: Define the shared browser branding presented by the Marketplace and documentation surfaces.

## Impact

The Nuxt Marketplace header and document head, VitePress theme configuration, static frontend assets, and shared-origin browser tests are affected. No API, CLI, dependency, or persistence behavior changes.
