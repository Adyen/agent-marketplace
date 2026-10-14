## Why

The Landing, Docs, and Marketplace surfaces previously used text-only or inconsistently sized product identity and did not publish the supplied product favicon. They should present one recognizable, accessible brand across independent frontend deployments and appearance modes.

## What Changes

- Display the supplied horizontal agent-marketplace logo in Landing, Docs, and Marketplace primary navigation.
- Select the black or white logo variant to preserve contrast with the active appearance.
- Render the visible logo artwork at one shared width and optical vertical alignment while leaving route-dependent navigation wrappers unchanged.
- Publish the supplied favicon from both frontend builds at their configured base paths.
- Preserve the accessible product name and existing home navigation behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-web-ui`: Define the shared browser branding presented by Landing, Docs, and Marketplace.

## Impact

Shared brand design tokens, Nuxt Marketplace navigation and document head, VitePress theme configuration, static frontend assets, and shared-origin browser tests are affected. No API, CLI, dependency, or persistence behavior changes.
