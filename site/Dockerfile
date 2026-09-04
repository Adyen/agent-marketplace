FROM node:22-alpine AS build

WORKDIR /site

RUN corepack enable && corepack install --global pnpm@10.4.1

COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY . .

ARG SITE_BASE=/
ENV SITE_BASE=${SITE_BASE}
RUN pnpm run build

FROM nginxinc/nginx-unprivileged:1.29-alpine

COPY --from=build /site/.vitepress/dist /usr/share/nginx/html

EXPOSE 8080
