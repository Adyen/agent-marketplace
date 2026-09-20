# How to configure a reverse proxy

Route public traffic to the Go API and Nuxt Marketplace UI while keeping operations endpoints private.

## Prerequisites

- A running Marketplace API and Nuxt UI.
- A reverse proxy such as Nginx.

## 1. Route public API paths to the Go API

Forward these paths to the Go API without rewriting the path. Use the upstream name without a trailing URI slash so the original path is preserved:

- `/.well-known/agent-marketplace.json`
- `/api/`
- `/healthz`

## 2. Route the Marketplace UI

Forward `/marketplace/` to the Nuxt application. Redirect the exact `/marketplace` path to `/marketplace/`.

## 3. Route the static landing site

Forward `/` to the VitePress static site.

## 4. Do not publish operations endpoints

`/readyz` and `/metrics` are served on a private listener. Do not route them through the public proxy. The Go API returns `404` if they are requested on the public listener.

## 5. Preserve and overwrite headers

Preserve `Host`, `X-Real-IP`, `X-Forwarded-For`, and `X-Forwarded-Proto`. At the first trusted proxy, set `X-Forwarded-For $remote_addr` so untrusted incoming chains cannot spoof the client address.

## 6. Admin panel authentication

If the database-managed admin panel is enabled with `admin.authentication.mode: oidc` (recommended), the application authenticates administrators itself through the OIDC provider; the proxy only routes `/marketplace/admin` and `/api/v1/admin/` and must not inject identity headers.

If the admin panel uses `trusted_proxy` mode instead, protect `/marketplace/admin` and `/api/v1/admin/` with your SSO gateway or identity-aware proxy. The reverse proxy must:
1. Strip incoming client-supplied identity headers (`X-Marketplace-Subject`, `X-Marketplace-Groups`).
2. Authenticate the operator and inject trusted identity headers.
3. Format the groups header as a JSON string array (for example `["marketplace-admins"]`).

See [How to enable and use the Marketplace admin panel](./enable-and-use-the-admin-panel.md) for full configuration details and proxy examples.

## Example Nginx snippet

```nginx
upstream marketplace_api {
    server app:8080;
}

upstream marketplace_web {
    server web:3000;
}

upstream landing_site {
    server site:8080;
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

    location = /marketplace {
        return 308 /marketplace/;
    }

    location /marketplace/ {
        proxy_pass http://marketplace_web;
        include /etc/nginx/proxy-headers.conf;
    }

    location / {
        proxy_pass http://landing_site;
        include /etc/nginx/proxy-headers.conf;
    }
}
```

Replace the upstream server addresses with your actual backend locations.

## Related documentation

- [How to enable and use the Marketplace admin panel](./enable-and-use-the-admin-panel.md)
- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [Marketplace health and metrics reference](../../reference/marketplace-health-and-metrics.md)
