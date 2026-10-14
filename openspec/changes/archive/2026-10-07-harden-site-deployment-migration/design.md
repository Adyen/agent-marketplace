## Context

See `proposal.md` for motivation. The current change already defines a root-mounted Marketplace profile, but the checked-in proxies, Helm chart, static documentation image, and OIDC route resolver carry independent path settings. Existing deployments also have persisted Helm values and browser links that use the former `site` and `/marketplace/` contracts.

## Goals / Non-Goals

**Goals:**

- Keep each deployment path setting explicit and validate mismatches at the earliest reliable boundary.
- Preserve old browser links during the route transition without proxying them to the wrong service.
- Make obsolete Helm values fail with actionable guidance instead of producing a partial deployment.
- Test route ownership through the actual consumer configuration rather than unrelated string presence.

**Non-Goals:**

- Supporting documentation at an arbitrary route other than `/docs/`.
- Changing machine-client JSON authentication errors or the legacy absent-browser-base API contract.
- Automatically translating arbitrary historical Helm values into new values.

## Decisions

1. **Preserve links with proxy redirects, not duplicate Nuxt mounts.** The supplied root-profile proxies redirect `/marketplace` and descendants to their root equivalents. This keeps one canonical web route and avoids serving the application under two bases. Redirects preserve queries and use a method-preserving status.

2. **Keep absent browser base as a legacy API contract.** The API continues to use `/marketplace` when `instance.browser_base` is absent. Root deployment examples configure a root browser base explicitly rather than changing the meaning of absence and breaking API-only or legacy deployments.

3. **Validate browser-base paths while loading Marketplace operator configuration.** URL validation rejects non-canonical paths before server construction; route resolution retains defensive fallback behavior for callers that construct configuration directly in tests or embedded use.

4. **Reject obsolete Helm keys.** Chart validation fails when `site` or `service.site` is present and names the replacement. Silent aliasing was rejected because merged defaults make explicit new settings and inherited old settings ambiguous, risking an unexpected workload.

5. **Keep documentation at `/docs/`, configure only its Marketplace return target.** A separate build argument supplies the Marketplace web-interface base used by embedded documentation navigation. This preserves deterministic path-contained assets while supporting an explicitly configured non-root web interface.

6. **Parse related Helm fields together in CI.** Assertions extract named environment variables and individual Ingress path/backend pairs so unrelated values cannot satisfy the contract.

## Risks / Trade-offs

- **Redirects can mask stale external gateway rules** → Document the gateway migration and test only the supplied proxy boundary.
- **Failing old Helm values interrupts an upgrade** → Fail before Kubernetes mutation and provide exact replacement keys in the error and upgrade guide.
- **Independent browser and web bases can drift in custom deployments** → Configure both in supplied defaults, document the invariant, and cover root and subpath flows in integration tests.
- **Encoded URL paths have multiple representations** → Validate both escaped and decoded path forms and retain defensive route checks.

## Migration Plan

1. Ship proxy redirects and upgrade guidance before operators switch the web interface to `/`.
2. Require operators to rename Helm values and align `instance.browser_base` with the web base; chart rendering fails before applying obsolete values.
3. Deploy API, web, and documentation services, then update external gateway rules and remove orphaned Compose services.
4. Retain redirects for the compatibility window; rollback restores the previous images and manifests without changing persisted Marketplace data.
