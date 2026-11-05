## Why

The root-mounted Marketplace deployment profile currently leaves upgrades unsafe: legacy `/marketplace/` links break, old Helm `site` values are silently ignored, and browser authentication can target routes that do not match the deployed web base. The deployment contract and its tests need explicit compatibility and fail-fast behavior before the profile can ship.

## What Changes

- Preserve legacy `/marketplace` browser links through same-origin redirects in the supplied root-mounted reverse-proxy profiles.
- Require Marketplace operator configuration and deployment examples to use a canonical browser base that agrees with browser authentication routes.
- Reject legacy Helm `site` values with an actionable migration error instead of silently omitting documentation.
- Make documentation navigation return to the configured Marketplace web base while retaining `/docs/` as the documentation route.
- Strengthen routing, authentication, generated-site, and Helm-rendering tests so they prove path ownership and migration behavior.
- Document the Compose, Helm, proxy, and OIDC upgrade steps.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `site-deployment-profiles`: Define compatibility redirects, configurable Marketplace-root navigation, and fail-fast deployment migration behavior.
- `marketplace-admin-auth`: Require canonical configured browser-base paths and prove login, callback, fallback, and unavailable-authentication routes follow that base.

## Impact

- Marketplace operator configuration validation and browser OIDC routing.
- Compose and Helm deployment examples, reverse-proxy configuration, chart validation, and upgrade guidance.
- VitePress documentation-only navigation and container build arguments.
- E2E, API, site, browser, container, and Helm contract tests.
