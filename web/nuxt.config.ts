import { devApiProxyTargets } from './app/lib/api-base'

const devApiProxy = process.env.NODE_ENV === 'development'
  ? devApiProxyTargets(process.env.NUXT_PUBLIC_API_BASE)
  : null

export default defineNuxtConfig({
  compatibilityDate: '2025-07-15',
  devtools: { enabled: false },
  modules: ['@nuxt/ui', '@nuxt/eslint'],
  css: ['~/assets/css/main.css'],
  // Icons resolve entirely from the locally installed `@iconify-json/lucide`
  // collection at build time. The app's own CSP sets `connect-src 'self'`, so
  // the remote Iconify API is unreachable by design and must never be a
  // dependency of rendering.
  icon: {
    serverBundle: 'local',
    clientBundle: { scan: true },
    fallbackToApi: false,
  },
  runtimeConfig: {
    // Server-only URL for Compose/internal-network SSR requests. Nuxt does not
    // serialize private runtimeConfig into the browser payload.
    apiBaseInternal: '',
    public: {
      apiBase: '/',
    },
  },
  typescript: {
    strict: true,
    typeCheck: true,
  },
  nitro: {
    preset: 'node-server',
    devProxy: devApiProxy
      ? {
          '/api/v1': { target: devApiProxy.api, changeOrigin: true },
          '/.well-known/agent-marketplace.json': { target: devApiProxy.wellKnown, changeOrigin: true },
        }
      : {},
  },
})
