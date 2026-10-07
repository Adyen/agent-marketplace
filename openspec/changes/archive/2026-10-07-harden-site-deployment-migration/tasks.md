## 1. Routing and authentication contracts

- [x] 1.1 Update the deployed proxy and Compose contract tests for root web, `/docs/`, API, and discovery ownership and verify the focused E2E tests pass
- [x] 1.2 Add root, subpath, absent-base, callback, missing-destination, and unavailable-authentication regressions with the required `marketplace-admin-auth` scenario annotations and verify the focused API tests fail before implementation
- [x] 1.3 Align supplied root-deployment browser-base configuration and OIDC routing behavior and verify the focused API tests pass
- [x] 1.4 Reject unsafe or non-canonical browser-base paths during Marketplace operator configuration loading and verify valid root and subpath controls still load

## 2. Deployment migration safety

- [x] 2.1 Add same-origin query-preserving `/marketplace` compatibility redirects to both supplied proxies, bind the `site-deployment-profiles` scenario, and verify the focused E2E routing test passes
- [x] 2.2 Reject obsolete Helm `site` and `service.site` values with replacement guidance, bump the chart version, bind the scenario, and verify current and legacy render cases
- [x] 2.3 Replace broad Helm CI string checks with named environment and paired Ingress route/backend assertions and verify the chart check catches mismatches
- [x] 2.4 Document Compose, Helm, proxy, browser-base, gateway, and orphan-container upgrade steps and verify examples use current names and routes

## 3. Documentation profile navigation

- [x] 3.1 Add a configurable Marketplace return base to the documentation-only build, bind the modified navigation scenario, and verify root and non-root generated links
- [x] 3.2 Make profile selection explicit in shared site chrome and reject inconsistent documentation profile/base arguments, then verify site and container checks
- [x] 3.3 Restore focused public landing-page browser coverage and verify public and Marketplace deployment profiles independently

## 4. Published contracts and verification

- [x] 4.1 Correct the OpenAPI administration destination contract, regenerate client types, update client comments, and verify API drift and OpenAPI tests
- [x] 4.2 Remove environment-specific hostnames from public documentation and update pull-request metadata with the verified scenarios
- [x] 4.3 Run `make validate`, `make spec-check`, `make validate-e2e`, and affected site, web, container, and Helm checks
- [x] 4.4 Run `openspec archive harden-site-deployment-migration --yes` as the final implementation step in this pull request
