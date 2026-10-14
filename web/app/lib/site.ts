export const SHARED_APPEARANCE_KEY = 'vitepress-theme-appearance'

export const appAssetPath = (baseURL: string, path: string) => `${baseURL.endsWith('/') ? baseURL : `${baseURL}/`}${path}`

// Documentation lives in the sibling VitePress site at the origin root.
export const DOCS_PATH = '/docs/'
