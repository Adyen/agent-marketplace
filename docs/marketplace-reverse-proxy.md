# Marketplace reverse proxy

The Go API and Nuxt application are separate HTTP services. Keep them on a private container network and publish only the reverse proxy. The proxy must terminate TLS and enforce the deployment's authentication and authorization policy; the application itself has no auth layer.

## Routing contract

Route these exact paths to the Go API without rewriting the path:

- `/.well-known/agent-marketplace.json`
- `/api/`
- `/healthz`

Route `/r/` (including both `/r/{registry}/{kind}/{name}` and `/r/{registry}/{kind}/{name}/revisions/{revision}`) and `/` to Nuxt. Nuxt owns the browser pages, while its same-origin API calls use `/api/v1`; the Go API must not be used as a fallback for `/r/**`. Preserve `Host`, `X-Real-IP`, `X-Forwarded-For`, and `X-Forwarded-Proto`. Set sensible proxy read, body-size, connection, and rate limits for the environment.

Do not route or publish `/readyz` or `/metrics`. Public requests to those exact paths are explicitly rejected with 404, while the paths are served on the operations listener and should be reachable only by a private monitoring/admin network. If a monitoring proxy is required, use a separate private listener and an explicit allowlist/authentication policy.

The examples in `deploy/compose/sqlite-filesystem` and `deploy/compose/postgres-s3` implement this contract with Nginx. Their published port is an example bind only; replace it with the organization's TLS and identity-aware edge. Do not add a public launch URL to initialization or committed example configuration.

## Header and cache guidance

Do not trust forwarded headers from untrusted clients; overwrite them at the first trusted proxy. In the bundled Nginx boundary, set `X-Forwarded-For $remote_addr` (not `$proxy_add_x_forwarded_for`) so an untrusted incoming chain cannot influence the application. Use HTTPS externally and configure HSTS only when all required hostnames are HTTPS. The API marks immutable artifact responses for caching, but caches must not bypass the edge's authentication policy or retain sensitive data beyond the organization's policy.

Expose only GET/HEAD routes supported by the application. Reject unexpected methods, oversized requests, malformed hostnames, and excessive concurrency at the edge. Health checks may be unauthenticated only when the health endpoint is isolated from sensitive data and protected by network policy.
