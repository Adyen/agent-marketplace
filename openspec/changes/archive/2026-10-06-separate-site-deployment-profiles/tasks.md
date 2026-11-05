## 1. Contract tests

- [x] 1.1 Add public-site build coverage for `public-project-site-exposes-only-landing-and-documentation`; prove `/` and `/docs/` exist, the expected public navigation works, and no local `/marketplace/` link or prefetch is emitted. Add the matching `// spec:` annotation and observe the intended red.
- [x] 1.2 Add documentation-only build coverage for `documentation-build-is-self-contained-below-docs`; prove all local documentation routes, assets, and fallback URLs remain below `/docs/`, allow only the declared profile-root navigation escape, and prove no landing route is published. Add the matching `// spec:` annotation and observe the intended red.
- [x] 1.3 Add a cross-profile comparison for `both-profiles-publish-the-same-documentation`; independently compare documentation route inventory and article content while excluding only declared profile chrome. Add the matching `// spec:` annotation and observe the intended red.
- [x] 1.4 Add deployed routing coverage for `marketplace-deployment-uses-root-ui-with-same-origin-services`; prove the root serves Nuxt, `/docs/` serves documentation, API and discovery requests reach the Go service, and the project landing page is unavailable. Add the matching `// spec:` annotation and observe the intended red.
- [x] 1.5 Add browser or rendered-output coverage for `navigation-respects-deployment-context`; prove public docs return to the public landing page, Marketplace docs return to the Marketplace root, and the Marketplace interface links to same-origin docs. Add the matching `// spec:` annotation and observe the intended red.
- [x] 1.6 Extend the existing OIDC browser failure and recovery regressions for root, explicit subpath, and absent browser-base configurations; prove same-origin redirects and safe fallbacks use only the configured browser-base path or the legacy fallback while JSON clients, validation, privacy, and session behavior remain unchanged. Retain the existing `marketplace-admin-auth` scenario annotations and observe the intended red.

## 2. Static site profiles

- [x] 2.1 Introduce deterministic public-site and documentation-only VitePress build modes over the shared `site/docs/` source, with the documentation-only output rooted entirely at `/docs/`.
- [x] 2.2 Make shared site chrome and navigation profile-aware without duplicating article content or selecting behavior from the runtime hostname.
- [x] 2.3 Remove the public landing and docs assumptions that a sibling Marketplace exists at `/marketplace/`, including navigation, hero action, prefetch, constants, components, and link assertions.
- [x] 2.4 Package public-site and documentation-only static images from the same source revision and add health checks that request a route each artifact actually owns.

## 3. Marketplace deployment profile

- [x] 3.1 Make `/` the default Marketplace web-interface base while preserving an explicit configurable subpath for operators that require one; keep same-origin API and discovery resolution working in both cases.
- [x] 3.2 Update the SQLite and PostgreSQL Compose topologies so the Marketplace profile serves Nuxt at `/`, documentation at `/docs/`, and reserved API, discovery, health, and authentication paths from their owning services without deploying the landing page.
- [x] 3.3 Update Helm values, services, ingress examples, validation, and render tests for a root Marketplace interface plus documentation-only service, including route precedence and independent enablement.
- [x] 3.4 Update the Marketplace interface and documentation navigation so each profile returns users to the root owned by that deployment.
- [x] 3.5 Derive same-origin OIDC browser error, administration fallback, and Marketplace return routes from only the path component of the validated configured Marketplace browser base, preserving legacy fallback paths when it is absent; update OpenAPI and generated client descriptions without changing machine-client JSON failures.

## 4. Documentation and CI

- [x] 4.1 Document the external `agent-marketplace.io/` plus `/docs/` topology and the generic self-hosted Marketplace root plus `/docs/` topology, using the Adyen internal hostname only as an environment-specific example where appropriate.
- [x] 4.2 Rewrite local Compose, production, reverse-proxy, Helm, architecture, and administration guidance so self-hosting deploys backend, frontend, and the shared docs without the project landing page.
- [x] 4.3 Update Make targets, change planning, container checks, and website/frontend workflows to build and validate both static profiles without weakening existing API drift, frontend, or site checks.

## 5. Verification

- [x] 5.1 Run focused site builds, frontend checks, browser tests, Compose configuration checks, container checks, and Helm rendering tests; verify both deployment profiles and configurable non-root Marketplace compatibility.
- [x] 5.2 Run `make validate`, `make spec-check`, and the domain gates selected by the changed deployment paths.
- [x] 5.3 Run `openspec archive separate-site-deployment-profiles --yes` as the final implementation step in the same pull request.
