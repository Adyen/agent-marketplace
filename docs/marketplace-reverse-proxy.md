# Marketplace reverse proxy

The Go API and the Nuxt application are separate HTTP services. Keep them on a private container network and publish only the reverse proxy. The proxy must terminate TLS and enforce the deployment's authentication and authorization policy. The application has no authentication layer apart from the optional admin API, which supports native OIDC sign-in (`admin.authentication.mode: oidc`) or identity asserted by a trusted proxy (`trusted_proxy`).

## Routing contract

Route these exact paths to the Go API without rewriting the path:

- `/.well-known/agent-marketplace.json`
- `/api/`
- `/healthz`

Route `/marketplace/` to Nuxt and `/` to the static VitePress site. Redirect the exact path `/marketplace` to `/marketplace/` so that Nuxt's asset and router base stays unambiguous. Marketplace browser pages then live at `/marketplace/r/{registry}/{kind}/{name}` and `/marketplace/r/{registry}/{kind}/{name}/revisions/{revision}`. Nuxt's same-origin API calls still use `/api/v1`. Do not use the Go API as a fallback for `/marketplace/**`. The bundled Compose configurations also forward `/_fonts/` to Nuxt. Preserve `Host`, `X-Real-IP`, `X-Forwarded-For`, and `X-Forwarded-Proto`. Set proxy read, body-size, connection, and rate limits that suit the environment.

The catalog projection route under `/api/v1/catalog` is an endpoint for internal consumers. It exists only when the operator sets `catalog_projection.enabled: true`. The service does not authenticate it, so publish it only behind the same internal, authenticated network boundary as the rest of the unauthenticated application API, and never expose it directly to the Internet. Pass `ETag` and `Cache-Control` response headers through unchanged, and forward `If-None-Match` unchanged on the first request of a scan. The proxy must not add, strip, replay, or cache conditional request headers across the cursor-based continuation requests.

Do not route or publish `/readyz` or `/metrics`. The public listener answers requests to those exact paths with 404; the real endpoints are served on the operations listener, which only a private monitoring or admin network should reach. If monitoring needs a proxy, use a separate private listener with an explicit allowlist or authentication policy.

The examples in `deploy/compose/sqlite-filesystem` and `deploy/compose/postgres-s3` implement this contract with Nginx. Set `instance.browser_base` to the public origin plus `/marketplace` so the browser route templates in the well-known document match the proxy path. The published port in those examples is only an example binding; replace it with the organization's TLS and identity-aware edge. Do not add a public launch URL to initialization or to committed example configuration.

## Admin routes

When `admin.enabled` is true, the API also serves `/api/v1/admin/**` under the same `/api/` route. Unlike the rest of the API, it accepts `POST` for `changes`, `validate`, `previews`, and OIDC `auth/logout`; allow those methods for admin paths at the edge. In trusted-proxy mode, authenticate both `/api/v1/admin/**` and the browser's `/marketplace/admin/**` at the gateway, strip any identity headers the client sends, and set them only after authentication. In OIDC mode, the gateway routes admin traffic without injecting identity headers. See [Enable and use the admin panel](../site/docs/how-to/operator/enable-and-use-the-admin-panel.md).

## Header and cache guidance

Do not trust forwarded headers from untrusted clients; overwrite them at the first trusted proxy. The bundled Nginx configuration sets `X-Forwarded-For $remote_addr` (not `$proxy_add_x_forwarded_for`) so that a chain sent by an untrusted client cannot influence the application. Use HTTPS externally, and configure HSTS only when every required hostname serves HTTPS. The API marks immutable artifact responses as cacheable, but caches must not bypass the edge's authentication policy or keep sensitive data longer than the organization's policy allows.

Outside the admin API, the application serves only `GET` and `HEAD` and answers other methods with 405. Reject unexpected methods, oversized requests, malformed hostnames, and excessive concurrency at the edge. Health checks may skip authentication only when the health endpoint exposes no sensitive data and network policy protects it.
