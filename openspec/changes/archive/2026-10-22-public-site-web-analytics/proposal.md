## Why

The public project website on GitHub Pages has no traffic measurement. Cloudflare Web Analytics is cookieless and needs no consent banner, so the published site can measure page views without privacy cost. The docs-only artifact ships inside self-hosted Marketplace containers and must never load third-party scripts.

## What Changes

- The publishing build of the public site emits the Cloudflare Web Analytics beacon on every page when `SITE_CLOUDFLARE_ANALYTICS_TOKEN` is set to a 32-character lowercase hex token. The token comes from a build environment variable (sourced from a repository variable in the Pages workflow), not from source.
- The docs profile, builds without a token, PR builds, and local builds emit no beacon.
- An invalid token fails the build before the token reaches the HTML.
- Site tests and the Website workflow verify both presence and absence of the beacon.

## Capabilities

### Modified Capabilities

- `site-deployment-profiles`: adds the public website analytics requirement.

## Impact

- `site/src/shared/analytics.ts` (new), `site/.vitepress/config.mts`, `site/package.json`, `site/tests/`.
- `.github/workflows/site.yml` passes the token only to the main publishing build and runs the analytics test.
- Requires the repository variable `CLOUDFLARE_WEB_ANALYTICS_TOKEN` to be set by a maintainer; without it the published site simply carries no beacon.
