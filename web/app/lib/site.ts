export const SHARED_APPEARANCE_KEY = 'vitepress-theme-appearance'

export const appAssetPath = (baseURL: string, path: string) => `${baseURL.endsWith('/') ? baseURL : `${baseURL}/`}${path}`

// Self-hosted documentation lives at the origin root, not below Nuxt's base.
export const DOCS_PATH = '/docs/'

export const MARKETPLACE_DOCS_URL = 'https://agent-marketplace.io/docs'

export const marketplaceDocsUrl = (enabled: unknown) => enabled === true || enabled === 'true' ? DOCS_PATH : MARKETPLACE_DOCS_URL
