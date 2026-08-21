export default defineNuxtConfig({
  compatibilityDate: '2025-07-15',
  devtools: { enabled: false },
  modules: ['@nuxt/ui', '@nuxt/eslint'],
  css: ['~/assets/css/main.css'],
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
  },
})
