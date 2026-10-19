## Context

The public project website and documentation-only VitePress image share
documentation sources. Helm already has `docs.enabled: false`; Compose
currently builds and starts docs on every deployment. The header links to
public docs while the footer and CLI setup links point to local docs.

## Decisions

Use `NUXT_PUBLIC_DOCS_ENABLED`, default false, to choose between the existing
public URL and origin-root `/docs/`. Resolve this through one shared helper
for the header, footer, and resource installation panel. Keep ordinary
anchors so Nuxt never handles documentation navigation.

Helm derives this setting from `docs.enabled`. Operators retain ownership
of their Ingress paths and add `/docs/` routing when enabling the service.

Compose uses `-f compose.docs.yaml` after its base file. The override adds
the existing documentation service, sets the web runtime flag, replaces
one docs-route include, and adds the proxy's healthy-docs dependency.
The base configuration has no docs service or dependency and returns 404
for local docs routes, with no DNS dependency on an absent docs service.
Shared Nginx includes avoid duplicating API, auth, and legacy-route policy.

## Alternatives

A global `airGapped` setting suggests disabling all network access, which
is outside this request. Independent service and link flags permit broken
combinations. Compose profiles alone cannot set another service's runtime
configuration, so an override is the smallest coherent opt-in.

## Verification

Unit tests cover the URL decision and false-like values. Browser tests
exercise default public links and enabled same-origin documentation,
including the footer and CLI setup link. Rendered Helm and Compose checks
cover both states. Isolated real-Nginx tests cover all supplied proxy
profiles, including startup without a docs hostname when disabled.

## Operational Limits

Operators must transfer or mirror images and dependencies before entering
an air-gapped network. Enabling docs does not disable external links in
articles, external registry access, OIDC, or other product networking.
