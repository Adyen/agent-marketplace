ARG NODE_IMAGE=node:24-alpine
ARG NGINX_IMAGE=nginxinc/nginx-unprivileged:1.29-alpine

FROM ${NODE_IMAGE} AS build

WORKDIR /site

RUN corepack enable && corepack install --global pnpm@10.4.1

COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY . .

ARG SITE_BASE=/
ENV SITE_BASE=${SITE_BASE}
RUN pnpm run build

FROM ${NGINX_IMAGE}

RUN printf 'server {\n\
    listen 8080;\n\
    location / {\n\
        root /usr/share/nginx/html;\n\
        try_files $uri $uri.html $uri/ =404;\n\
    }\n\
    error_page 404 /404.html;\n\
}\n' > /etc/nginx/conf.d/default.conf

COPY --from=build /site/.vitepress/dist /usr/share/nginx/html

EXPOSE 8080
