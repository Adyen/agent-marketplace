# How to configure a reverse proxy

Route public traffic to the Go API, the Nuxt Marketplace UI, and optionally the documentation-only VitePress image while keeping operations endpoints private.

## Prerequisites

- A running Marketplace API and Nuxt UI, plus a documentation service if local docs are enabled.
- A reverse proxy such as Nginx.
- TLS and an authentication boundary appropriate for your deployment.

## 1. Route public API paths to the Go API

Forward these paths to the Go API without rewriting them. In Nginx, write `proxy_pass` with the upstream name and no trailing URI slash so the original path reaches the API:

- `/.well-known/agent-marketplace.json`
- `/api/`
- `/healthz`

## 2. Route documentation to VitePress

Local docs are disabled by default. To enable them, use Helm `docs.enabled: true`
or the supplied Compose `compose.docs.yaml` override. These also set
`NUXT_PUBLIC_DOCS_ENABLED=true` for the UI.

When enabled, redirect the exact `/docs` path to `/docs/`, then forward `/docs/`
and its descendants to the documentation service. The documentation image
contains no project landing page and owns no route outside `/docs/`.
When disabled, do not configure a docs upstream: the supplied Compose proxies
return 404 for local docs and UI links use `https://agent-marketplace.io/docs`.

## 3. Route the Marketplace UI at the origin root

Forward the remaining `/` traffic to the Nuxt application. This includes the Marketplace catalog, resource pages, `/admin`, `/auth/error`, and Nuxt assets.

Set `instance.browser_base` to the public origin without a path suffix, for example `https://marketplace.example.com`. Set the Nuxt application base to `/`.

## 4. Keep operations endpoints private

The API serves `/readyz` and `/metrics` on the operations listener (`operations_listen`, `127.0.0.1:9090` by default). Do not route them through the public proxy. If a request for either path reaches the public listener, the API returns `404`.

## 5. Set forwarding headers

Preserve `Host`, `X-Real-IP`, `X-Forwarded-For`, and `X-Forwarded-Proto`. At the first trusted proxy, set `X-Forwarded-For $remote_addr` so a client cannot spoof its address with a forged incoming chain.

## 6. Configure admin panel authentication

With `admin.authentication.mode: oidc`, the application signs administrators in through the configured identity provider. Route `/admin`, `/auth/error`, and `/api/v1/admin/` to their normal UI and API upstreams. Do not inject identity headers.

With `trusted_proxy` mode, protect `/admin` and `/api/v1/admin/` with your single sign-on gateway or identity-aware proxy. The API accepts identity headers only from peer addresses listed in `admin.trusted_proxies` and reads them from the configured `admin.subject_header` and `admin.groups_header`. The proxy must:

1. Remove client-supplied copies of the identity headers.
2. Authenticate the operator.
3. Inject the authenticated subject and groups.
4. Encode the groups header as a JSON array of strings, such as `["marketplace-admins"]`.

[How to enable and use the Marketplace admin panel](./enable-and-use-the-admin-panel.md) has the complete configuration and authentication flow.

## 7. Configure Nginx

This example shows the enabled-docs routing from [`deploy/compose/sqlite-filesystem/nginx.conf`](https://github.com/Adyen/agent-marketplace/blob/main/deploy/compose/sqlite-filesystem/nginx.conf) and its `marketplace-docs.conf` include together. The included `proxy-headers.conf` sets the headers from step 5. Remove the docs upstream and docs locations when documentation is disabled:

```nginx
upstream marketplace_api {
    server app:8080;
}

upstream marketplace_web {
    server web:3000;
}

upstream marketplace_docs {
    server docs:8080;
}

server {
    listen 8080;
    server_name _;
    absolute_redirect off;

    location = /.well-known/agent-marketplace.json {
        proxy_pass http://marketplace_api;
        include /etc/nginx/proxy-headers.conf;
    }

    location /api/ {
        proxy_pass http://marketplace_api;
        include /etc/nginx/proxy-headers.conf;
    }

    location = /healthz {
        proxy_pass http://marketplace_api;
        include /etc/nginx/proxy-headers.conf;
    }

    location = /readyz { return 404; }
    location = /metrics { return 404; }

    location = /docs {
        return 308 /docs/;
    }

    location /docs/ {
        proxy_pass http://marketplace_docs;
        include /etc/nginx/proxy-headers.conf;
    }

    location / {
        proxy_pass http://marketplace_web;
        include /etc/nginx/proxy-headers.conf;
    }
}
```

Replace the upstream server addresses with your service locations. Put authentication controls around the required paths or the complete origin without changing this route ownership.

## Related documentation

- [How to deploy a production Marketplace](./deploy-a-production-marketplace.md)
- [How to enable and use the Marketplace admin panel](./enable-and-use-the-admin-panel.md)
- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [Marketplace health and metrics reference](../../reference/marketplace-health-and-metrics.md)
