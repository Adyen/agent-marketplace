ARG NODE_IMAGE=node:24-alpine

FROM ${NODE_IMAGE} AS build
ARG NPM_CONFIG_REGISTRY
WORKDIR /src/web
COPY --chown=1000:1000 package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN npm install --global "$(node -p 'require("./package.json").packageManager')" && npm cache clean --force
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY --chown=1000:1000 . .
COPY --from=design --chown=1000:1000 semantic-colors.css /src/design/semantic-colors.css
COPY --from=design --chown=1000:1000 marketplace-navigation.css /src/design/marketplace-navigation.css
RUN pnpm build

FROM ${NODE_IMAGE} AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    NITRO_HOST=0.0.0.0 \
    NITRO_PORT=3000 \
    NUXT_TELEMETRY_DISABLED=1
COPY --from=build --chown=1000:1000 /src/web/.output ./.output
USER 1000:1000
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD node -e "fetch('http://127.0.0.1:3000' + (process.env.NUXT_APP_BASE_URL || '/')).then(r => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))"
CMD ["node", ".output/server/index.mjs"]
