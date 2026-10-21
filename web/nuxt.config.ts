import { devApiProxyTargets } from './app/lib/api-base'
import { SHARED_APPEARANCE_KEY } from './app/lib/site'
import { fileURLToPath, URL as NodeURL } from 'node:url'

const workspaceRoot = fileURLToPath(new NodeURL('..', import.meta.url))

const devApiProxy = process.env.NODE_ENV === 'development'
  ? devApiProxyTargets(process.env.NUXT_PUBLIC_API_BASE)
  : null

export default defineNuxtConfig({
  compatibilityDate: '2025-07-15',
  devtools: { enabled: false },
  modules: ['@nuxt/ui', '@nuxt/eslint'],
  css: ['~/assets/css/main.css'],
  // Nuxt color mode persists `system`; VitePress persists `auto`. Keep one
  // small controller so both applications can honor the same stored values.
  ui: {
    colorMode: false,
    fonts: false,
  },
  app: {
    head: {
      script: [{
        id: 'check-shared-appearance',
        innerHTML: `;(() => {
          let preference = 'auto'
          try {
            preference = localStorage.getItem('${SHARED_APPEARANCE_KEY}') || 'auto'
          } catch {}
          const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
          if (preference === 'dark' || (preference === 'auto' && prefersDark)) {
            document.documentElement.classList.add('dark')
          }
        })()`,
      }],
    },
  },
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
      docsEnabled: false,
    },
  },
  typescript: {
    strict: true,
    typeCheck: true,
  },
  vite: {
    server: {
      fs: {
        allow: [workspaceRoot],
      },
    },
  },
  routeRules: {
    '/admin': { ssr: false, prerender: false, headers: { 'Cache-Control': 'no-store', 'X-Robots-Tag': 'noindex, nofollow, noarchive' } },
    '/admin/**': { ssr: false, prerender: false, headers: { 'Cache-Control': 'no-store', 'X-Robots-Tag': 'noindex, nofollow, noarchive' } },
    '/auth/error': { ssr: true, prerender: false, headers: { 'Cache-Control': 'no-store', 'X-Robots-Tag': 'noindex, nofollow, noarchive', 'Referrer-Policy': 'no-referrer' } },
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
