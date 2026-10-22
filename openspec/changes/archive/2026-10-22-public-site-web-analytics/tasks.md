## 1. Implementation

- [x] 1.1 Add `site/src/shared/analytics.ts` with the token check and `analyticsHead` for the public profile and the no-op docs profile.
- [x] 1.2 Append `analyticsHead` to the `head` array in `site/.vitepress/config.mts`.
- [x] 1.3 Pass `SITE_CLOUDFLARE_ANALYTICS_TOKEN` to the publishing build step in `.github/workflows/site.yml` and run `tests/analytics.test.ts` there and in `test:public`.

## 2. Tests

- [x] 2.1 Add `site/tests/analytics.test.ts` for the public beacon, the no-analytics cases, and invalid tokens.
- [x] 2.2 Assert in `site/tests/docs-profile.test.ts` that no docs-only HTML contains the beacon.
- [x] 2.3 Assert in `site/tests/public-site.test.ts` that the beacon and token appear when configured and are absent otherwise.

## 3. Verification

- [x] 3.1 Run the site tests with and without the token for both profiles, and confirm an invalid token fails the build.
- [x] 3.2 Run `make spec-check`.
