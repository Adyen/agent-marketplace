# How to configure a reverse proxy

Route public traffic to the Go API and the Nuxt Marketplace UI, and keep the operations endpoints private.

## Prerequisites

- A running Marketplace API and Nuxt UI.
- A reverse proxy such as Nginx.

## 1. Route public API paths to the Go API

Forward these paths to the Go API without rewriting them. In Nginx, write `proxy_pass` with the upstream name and no trailing URI slash so the original path reaches the API:

- `/.well-known/agent-marketplace.json`
- `/api/`
- `/healthz`

## 2. Route the Marketplace UI

Forward `/marketplace/` to the Nuxt application and redirect the exact path `/marketplace` to `/marketplace/`. The Compose configurations in the repository also forward `/_fonts/` to the Nuxt application.

## 3. Route the static landing site

Forward `/` to the VitePress static site.

## 4. Keep operations endpoints private

The API serves `/readyz` and `/metrics` on the operations listener (`operations_listen`, `127.0.0.1:9090` by default). Do not route them through the public proxy. If a request for either path reaches the public listener, the API returns `404`.

## 5. Set forwarding headers

Preserve `Host`, `X-Real-IP`, `X-Forwarded-For`, and `X-Forwarded-Proto`. At the first trusted proxy, set `X-Forwarded-For $remote_addr` so a client cannot spoof its address with a forged incoming chain.

## 6. Admin panel authentication

With the database-managed admin panel enabled and `admin.authentication.mode: oidc` (recommended), the application signs administrators in through the OIDC provider itself. The proxy only routes `/marketplace/admin` and `/api/v1/admin/`, and must not inject identity headers.

With `trusted_proxy` mode, protect `/marketplace/admin` and `/api/v1/admin/` with your SSO gateway or identity-aware proxy. The API accepts identity headers only from peer addresses listed in `admin.trusted_proxies`, and reads them from the header names set in `admin.subject_header` and `admin.groups_header` (for example `X-Marketplace-Subject` and `X-Marketplace-Groups`). The reverse proxy must:

1. Strip any client-supplied copies of those identity headers.
2. Authenticate the operator and inject trusted identity headers.
3. Format the groups header as a JSON array of strings (for example `["marketplace-admins"]`).

[How to enable and use the Marketplace admin panel](./enable-and-use-the-admin-panel.md) has the full configuration and proxy examples.

## Example Nginx configuration

This is the routing from [`deploy/compose/sqlite-filesystem/nginx.conf`](https://github.com/Adyen/agent-marketplace/blob/main/deploy/compose/sqlite-filesystem/nginx.conf) without its comments. The included `proxy-headers.conf` sets the headers from step 5.

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

    location /_fonts/ {
        proxy_pass http://marketplace_web;
        include /etc/nginx/proxy-headers.conf;
    }

    location / {
        proxy_pass http://landing_site;
        include /etc/nginx/proxy-headers.conf;
    }
}
```

Replace the upstream server addresses with the locations of your backends.

## Related documentation

- [How to enable and use the Marketplace admin panel](./enable-and-use-the-admin-panel.md)
- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [Marketplace health and metrics reference](../../reference/marketplace-health-and-metrics.md)
