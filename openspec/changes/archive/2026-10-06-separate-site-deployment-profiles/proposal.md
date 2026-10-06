## Why

The project website and a deployed Marketplace currently share a combined routing model: the landing page owns `/`, documentation lives below `/docs/`, and the Marketplace web interface lives below `/marketplace/`. That model does not match the intended products. The public project website should contain only the landing page and documentation, while each self-hosted Marketplace should present its web interface at the origin root and the same documentation below `/docs/` without exposing the project landing page.

## What Changes

- Define separate public-project-site and Marketplace deployment profiles from the same repository.
- Keep the public project website at `/` with documentation at `/docs/`, without advertising a sibling hosted Marketplace route.
- Serve the Marketplace web interface at `/`, the Marketplace API and discovery routes at their existing same-origin paths, and documentation at `/docs/` in a Marketplace deployment.
- Produce a documentation-only static artifact whose routes and assets are self-contained below `/docs/`.
- Publish the same documentation page set and article content in both deployment profiles while allowing profile-specific navigation.
- Update Compose, Helm, proxy examples, CI, and operator documentation to prove both profiles independently.

## Capabilities

### New Capabilities

- `site-deployment-profiles`: Observable routing, documentation parity, and navigation behavior for the public project website and self-hosted Marketplace deployments.

### Modified Capabilities

- `marketplace-admin-auth`: Resolve browser error and recovery routes beneath the configured Marketplace web base instead of the fixed `/marketplace/` prefix.

## Impact

- VitePress configuration, build commands, static images, and site navigation.
- Marketplace web-interface base-path defaults and documentation links.
- Compose and Helm services, ingress examples, and reverse-proxy routing.
- Website, frontend, container, Helm, and browser validation.
- Public deployment and Marketplace operator documentation.
- No source code is made private, and no Marketplace storage, resource, authentication-policy, session, or JSON error contract changes; browser authentication redirect locations follow the configured Marketplace web base.
