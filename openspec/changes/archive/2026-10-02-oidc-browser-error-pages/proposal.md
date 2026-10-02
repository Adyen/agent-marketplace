## Why

Browser OIDC administration failures currently leave users on raw JSON responses instead of giving them a recovery action. The admin UI cannot format errors after the browser navigates to login or callback endpoints.

## What Changes

- Negotiate browser-only error redirects while preserving JSON error statuses and bodies.
- Add a dedicated server-rendered sign-in error page with safe fresh-login recovery.
- Apply privacy and security headers and update the contract, documentation, and tests.

## Capabilities

### New Capabilities

- `marketplace-admin-auth`: Browser presentation and recovery for OIDC administration sign-in errors.

### Modified Capabilities

- None.

## Impact

Affected systems include `pkg/marketplace/api` administration authentication routes and protections, the Nuxt auth/error route and security headers, OpenAPI and generated types, and the operator administration guide. No new dependencies or authorization changes are introduced.
