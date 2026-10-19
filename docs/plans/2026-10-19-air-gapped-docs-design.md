# Opt-in Marketplace documentation

Serve the existing documentation-only image alongside a Marketplace only
when the operator opts in. Public documentation remains the default.

- Helm: `docs.enabled: true` deploys docs and selects local links.
- Compose: append `-f compose.docs.yaml` to enable the service, routing,
  and local links together in either supplied deployment.
- Nuxt runtime: `NUXT_PUBLIC_DOCS_ENABLED`, default false, controls every
  documentation entry point without rebuilding the web image.
- Local docs always live at origin-root `/docs/`, not below the Nuxt base.
- Base Compose proxies return 404 for local docs and start without docs.

Reuse the current VitePress artifact and shared documentation source.
Do not add a Go-server docs flag: frontend deployment owns this policy.
Do not call this a global offline mode or change unrelated networking.

The normative change is `openspec/changes/opt-in-marketplace-docs`.
