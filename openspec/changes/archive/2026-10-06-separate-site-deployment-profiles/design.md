## Context

The repository contains two independent web applications. VitePress builds the project landing page and documentation, while Nuxt builds the Marketplace web interface. The current combined Compose topology puts VitePress at `/`, documentation at `/docs/`, and Nuxt at `/marketplace/`. The landing page and its navigation therefore assume that a Marketplace is deployed beside the project website.

The intended external deployment is `https://agent-marketplace.io/` for the landing page and `https://agent-marketplace.io/docs/` for documentation. A self-hosted Marketplace, including the intended internal deployment, instead needs the Marketplace web interface at its origin root and the same documentation at `/docs/`; for example, `https://marketplace.example/` and `https://marketplace.example/docs/`.

## Goals / Non-Goals

**Goals:**
- Make the public project website and a self-hosted Marketplace explicit, independently testable deployment profiles.
- Keep one documentation source and publish equivalent documentation through both profiles.
- Let a Marketplace own `/` while reserving `/docs/`, `/api/`, discovery, health, and authentication paths for their intended services.
- Keep documentation assets beneath `/docs/` so a root-mounted Marketplace cannot shadow or intercept them.
- Make navigation return users to the correct root for the active profile.

**Non-Goals:**
- Making the Marketplace frontend or backend private.
- Operating a public hosted Marketplace at `agent-marketplace.io`.
- Forking or maintaining a separate internal documentation corpus.
- Changing Marketplace storage, indexing, authentication policy, sessions, or machine-client JSON behavior.
- Requiring the project landing page in a self-hosted Marketplace deployment.

## Decisions

1. **Build two artifacts from one documentation source.** The public-site artifact contains the landing page at `/` and documentation at `/docs/`. The documentation-only artifact contains no landing route and is built for `/docs/`, including its generated assets and fallback page. Both consume the checked-in files under `site/docs/`; profile-specific chrome and links may differ, but page inventory and article content may not.

2. **Give the Marketplace web interface the origin root in Marketplace deployments.** Nuxt runs with `/` as its application base and uses the same origin for API requests. The reverse proxy or ingress sends `/api/`, `/.well-known/`, health, and `/docs/` requests to their dedicated services before sending the remaining `/` traffic to Nuxt. Browser-only routes such as `/auth/error` remain with Nuxt. The static project landing page is absent from this profile.

3. **Keep the public website product-neutral.** The external landing page and documentation do not link to a local `/marketplace/` application or prefetch it. They link to documentation, source, installation, and self-hosting guidance instead. This prevents a dead route and avoids implying that the project operates a public Marketplace.

4. **Make navigation and browser authentication routes follow the deployment base.** Public documentation returns to the public landing page. Documentation embedded in a Marketplace returns to that Marketplace's root. The Marketplace web interface links to same-origin documentation below `/docs/`. OIDC browser failures and recovery targets are derived only from the path component of the validated configured Marketplace browser base, so redirects remain same-origin: a root deployment uses `/auth/error` and `/admin`, while an explicit `/marketplace/` deployment uses `/marketplace/auth/error` and `/marketplace/admin`. An API-only or legacy configuration without a browser base retains the existing `/marketplace` fallback paths. Build-time profile selection is preferred over runtime hostname checks for static navigation so generated output is deterministic and deployable on arbitrary operator domains.

5. **Keep packaging responsibilities separate.** The public-site image serves landing plus documentation. A documentation-only image serves the `/docs/` artifact. Marketplace deployment examples compose the API/worker, Nuxt, and documentation-only images; public website deployment uses only the public-site image. Both static images are produced from the same source revision.

6. **Prove route ownership rather than relying on configuration inspection.** Tests build both static profiles and inspect their generated routes and links. Container or browser coverage requests the deployed paths and proves that the Marketplace root, documentation, API/discovery routes, and absence of the landing page are real. Helm rendering tests prove that longest-prefix routing sends each path to the intended service.

## Risks / Trade-offs

- **Two static artifacts can drift.** Build both from the same files in one workflow and compare their documentation route inventory and article content.
- **A root UI can shadow reserved paths.** Route API, discovery, health, authentication, and documentation prefixes explicitly and cover them through the deployed proxy or ingress.
- **VitePress can emit root-level assets.** The documentation-only build must use `/docs/` as its effective base and fail validation when generated local URLs escape that prefix.
- **Contextual navigation can alter article output.** Permit differences only in profile chrome and navigation; compare documentation bodies or another independently defined content projection.
- **Existing `/marketplace/` deployments may rely on the old default.** Keep an explicit non-root base configurable for operators, document the default change, and test both root and configured subpath behavior where compatibility is promised.

## Migration Plan

1. Add failing build and routing tests for both deployment profiles and bind every scenario in the delta specification.
2. Add deterministic public-site and documentation-only build modes and profile-aware navigation.
3. Move the Marketplace deployment default to a root-mounted Nuxt interface while retaining an explicit base-path override.
4. Update Compose and Helm to deploy documentation without the landing page and route reserved prefixes ahead of the Nuxt fallback.
5. Remove public-site `/marketplace/` links and update self-hosting and website deployment documentation.
6. Build and exercise both profiles, run frontend/container/Helm checks and `make spec-check`, then archive this change in the implementation pull request.

Rollback selects the prior image and deployment manifests. No persisted state, Marketplace operator configuration, consumer configuration, or API data requires migration.
