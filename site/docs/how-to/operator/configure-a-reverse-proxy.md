# How to configure a reverse proxy

This guide shows how to route public traffic to the Go API and Nuxt Marketplace UI while keeping operations endpoints private.

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

- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [Marketplace health and metrics reference](../../reference/marketplace-health-and-metrics.md)
