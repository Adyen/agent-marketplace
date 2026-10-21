ARG NODE_IMAGE=node:24-alpine
ARG NGINX_IMAGE=nginxinc/nginx-unprivileged:1.29-alpine

FROM ${NODE_IMAGE} AS build

USER root

RUN npm install --global pnpm@10.26.0 && npm cache clean --force

RUN mkdir -p /site /design && chown -R 1000:1000 /site /design
WORKDIR /site
USER 1000

COPY --chown=1000:1000 package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY --chown=1000:1000 . .
COPY --from=design --chown=1000:1000 semantic-colors.css brand.css foundations.css /design/
COPY --from=design --chown=1000:1000 fonts/ /design/fonts/

ARG SITE_BASE=/
ARG SITE_PROFILE=public
ARG MARKETPLACE_BASE=/
ENV SITE_BASE=${SITE_BASE} \
    SITE_PROFILE=${SITE_PROFILE} \
    MARKETPLACE_BASE=${MARKETPLACE_BASE}
RUN if [ "${SITE_PROFILE}" = "docs" ] && [ "${SITE_BASE}" != "/docs/" ]; then \
        echo "docs profile requires SITE_BASE=/docs/" >&2; exit 1; \
    elif [ "${SITE_PROFILE}" = "docs" ]; then \
        pnpm run build:docs && output=.vitepress/dist-docs || exit 1; \
    elif [ "${SITE_PROFILE}" = "public" ]; then \
        pnpm run build && output=.vitepress/dist || exit 1; \
    else \
        echo "unsupported SITE_PROFILE: ${SITE_PROFILE}" >&2; exit 1; \
    fi; \
    mkdir .site-dist && cp -R "${output}/." .site-dist/

FROM ${NGINX_IMAGE} AS runtime

ARG SITE_BASE=/
ENV SITE_BASE=${SITE_BASE}

RUN root_guard=; \
    if [ "${SITE_BASE}" != "/" ]; then root_guard='    location = / { return 404; }'; fi; \
    printf 'server {\n\
    listen 8080;\n\
%s\n\
    location / {\n\
        root /usr/share/nginx/html;\n\
        try_files $uri $uri.html $uri/ =404;\n\
    }\n\
    error_page 404 %s404.html;\n\
}\n' "${root_guard}" "${SITE_BASE}" > /etc/nginx/conf.d/default.conf

COPY --from=build /site/.site-dist/ /usr/share/nginx/html${SITE_BASE}

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD if command -v wget >/dev/null 2>&1; then \
        wget -q -O - -T 5 "http://127.0.0.1:8080${SITE_BASE}" >/dev/null; \
    elif command -v curl >/dev/null 2>&1; then \
        curl -fsS --max-time 5 "http://127.0.0.1:8080${SITE_BASE}" >/dev/null; \
    else \
        exit 1; \
    fi
