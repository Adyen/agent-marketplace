# Marketplace reverse proxy

The Go API and Nuxt application are separate HTTP services. Keep them on a private container network and publish only the reverse proxy. The proxy must terminate TLS and enforce the deployment's authentication and authorization policy. The application itself has no auth layer except for the optional admin surface, which supports native OIDC sign-in (`admin.authentication.mode: oidc`) or trusted-proxy identity assertion.

## Routing contract

Route these exact paths to the Go API without rewriting the path:

- `/.well-known/agent-marketplace.json`
- `/api/`
- `/healthz`

Route `/marketplace/` to Nuxt and `/` to the static VitePress site. Redirect the exact `/marketplace` path to `/marketplace/`, so Nuxt's asset and router base remains unambiguous. Marketplace browser pages therefore live at `/marketplace/r/{registry}/{kind}/{name}` and `/marketplace/r/{registry}/{kind}/{name}/revisions/{revision}`. Nuxt's same-origin API calls still use `/api/v1`; the Go API must not be used as a fallback for `/marketplace/**`. Preserve `Host`, `X-Real-IP`, `X-Forwarded-For`, and `X-Forwarded-Proto`. Set sensible proxy read, body-size, connection, and rate limits for the environment.

The catalog projection route under `/api/v1/catalog` is an internal consumer endpoint. It exists only when the operator explicitly enables `catalog_projection.enabled: true`. The service does not authenticate it, so publish it only behind the same internal, authenticated network boundary as the rest of the unauthenticated application API; never expose it directly to the Internet. Preserve `ETag` and `Cache-Control` responses unchanged, and forward `If-None-Match` unchanged for initial catalog requests. The proxy must not add, strip, replay, or cache conditional request headers across keyset continuation requests.

Do not route or publish `/readyz` or `/metrics`. Public requests to those exact paths are explicitly rejected with 404, while the paths are served on the operations listener and should be reachable only by a private monitoring/admin network. If a monitoring proxy is required, use a separate private listener and an explicit allowlist/authentication policy.

The examples in `deploy/compose/sqlite-filesystem` and `deploy/compose/postgres-s3` implement this contract with Nginx. Set `instance.browser_base` to the public origin plus `/marketplace`, so well-known browser route templates match the proxy path. Their published port is an example bind only; replace it with the organization's TLS and identity-aware edge. Do not add a public launch URL to initialization or committed example configuration.

## Header and cache guidance

Do not trust forwarded headers from untrusted clients; overwrite them at the first trusted proxy. In the bundled Nginx boundary, set `X-Forwarded-For $remote_addr` (not `$proxy_add_x_forwarded_for`) so an untrusted incoming chain cannot influence the application. Use HTTPS externally and configure HSTS only when all required hostnames are HTTPS. The API marks immutable artifact responses for caching, but caches must not bypass the edge's authentication policy or retain sensitive data beyond the organization's policy.

Expose only GET/HEAD routes supported by the application. Reject unexpected methods, oversized requests, malformed hostnames, and excessive concurrency at the edge. Health checks may be unauthenticated only when the health endpoint is isolated from sensitive data and protected by network policy.
