ARG NODE_IMAGE=node:24-alpine
ARG NGINX_IMAGE=nginxinc/nginx-unprivileged:1.29-alpine

FROM ${NODE_IMAGE} AS build

USER root

RUN npm install --global pnpm@10.26.0 && npm cache clean --force

RUN mkdir -p /site /design && chown -R 1000:1000 /site /design
WORKDIR /site
USER 1000

COPY --chown=1000:1000 package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY --chown=1000:1000 . .
COPY --from=design --chown=1000:1000 semantic-colors.css /design/semantic-colors.css

ARG SITE_BASE=/
ENV SITE_BASE=${SITE_BASE}
RUN pnpm run build

FROM ${NGINX_IMAGE} AS runtime

ARG SITE_BASE=/

RUN printf 'server {\n\
    listen 8080;\n\
    location / {\n\
        root /usr/share/nginx/html;\n\
        try_files $uri $uri.html $uri/ =404;\n\
    }\n\
    error_page 404 %s404.html;\n\
}\n' "${SITE_BASE}" > /etc/nginx/conf.d/default.conf

COPY --from=build /site/.vitepress/dist/ /usr/share/nginx/html${SITE_BASE}

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O - -T 5 http://127.0.0.1:8080/ >/dev/null || exit 1
