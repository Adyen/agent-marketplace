import { defineConfig } from 'vitepress'
import { DOCS_PATH as PUBLIC_DOCS_PATH, GITHUB_REPOSITORY_URL } from '../src/shared/site'
import { writeLlmsFiles } from './llms'
import { docsSidebar } from './sidebar'

const docsOnly = process.env.SITE_PROFILE === 'docs'

function deploymentBase(name: string, value: string): string {
  if (!value.startsWith('/') || !value.endsWith('/') || value.startsWith('//') || value.includes('\\') || /[?#\r\n]/.test(value) || value.split('/').some(part => part === '.' || part === '..')) {
    throw new Error(`${name} must be a canonical absolute path ending in /`)
  }
  return value
}

// Pages supplies SITE_BASE for its project URL or custom domain; standalone
// builds can select the same deployment path without changing content links.
const base = docsOnly ? '/docs/' : deploymentBase('SITE_BASE', process.env.SITE_BASE ?? '/')
const marketplaceBase = docsOnly ? deploymentBase('MARKETPLACE_BASE', process.env.MARKETPLACE_BASE ?? '/') : '/'
const DOCS_PATH = docsOnly ? '/' : PUBLIC_DOCS_PATH
const publicAsset = (path: string) => `${base}${path}`

const repo = GITHUB_REPOSITORY_URL

const sidebar = docsSidebar(DOCS_PATH)

export default defineConfig({
  base,
  srcDir: '.',
  srcExclude: docsOnly ? ['index.md', 'README.md'] : [],
  rewrites: docsOnly ? id => id.startsWith('docs/') ? id.slice('docs/'.length) : id : undefined,
  outDir: docsOnly ? '.vitepress/dist-docs' : '.vitepress/dist',
  cacheDir: docsOnly ? '.vitepress/cache-docs' : '.vitepress/cache',
  lang: 'en-US',
  title: 'agent-marketplace',
  description:
    'A package manager for AI agent tooling. Distribute skills, commands, subagents and MCP servers from git registries into the native layout of every AI coding harness you use.',
  cleanUrls: true,
  appearance: true,

  buildEnd: siteConfig => writeLlmsFiles({
    base,
    description: siteConfig.site.description,
    docsPath: DOCS_PATH,
    outDir: siteConfig.outDir,
    sidebar,
    srcDir: siteConfig.srcDir,
    title: siteConfig.site.title,
  }),

  vite: {
    server: {
      // Build output lives inside the site directory, so exclude it from the
      // dev watcher: otherwise a production build triggers a reload per page.
      watch: { ignored: ['**/.vitepress/dist/**', '**/.vitepress/cache/**'] }
    }
  },

  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: publicAsset('brand/favicon.svg') }],
    ['link', { rel: 'icon', type: 'image/png', sizes: '32x32', href: publicAsset('brand/favicon-32x32.png') }],
    ['link', { rel: 'icon', type: 'image/png', sizes: '64x64', href: publicAsset('brand/favicon-64x64.png') }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'agent-marketplace' }],
    ['meta', { property: 'og:image', content: 'https://agent-marketplace.io' + publicAsset('brand/social-preview.png') }],
    ['meta', { property: 'og:image:type', content: 'image/png' }],
    ['meta', { property: 'og:image:width', content: '960' }],
    ['meta', { property: 'og:image:height', content: '960' }],
    ['meta', { property: 'og:image:alt', content: 'agent-marketplace logo' }],
    ['meta', { name: 'twitter:card', content: 'summary_large_image' }]
  ],

  themeConfig: {
    logo: {
      light: '/brand/logo-horizontal-black.svg',
      dark: '/brand/logo-horizontal-white.svg',
      alt: 'agent-marketplace'
    },
    siteTitle: false,
    siteProfile: docsOnly ? 'docs' : 'public',

    nav: docsOnly
      ? [
          { component: 'SiteNavLink', props: { label: 'Marketplace', href: marketplaceBase } },
          { component: 'SiteNavLink', props: { label: 'Docs', href: '/docs/' } }
        ]
      : [{ text: 'Docs', link: DOCS_PATH, activeMatch: `^${DOCS_PATH}` }],

    sidebar: {
      [DOCS_PATH]: sidebar
    },

    search: { provider: 'local' },

    editLink: {
      pattern: `${repo}/edit/main/site/:path`,
      text: 'Edit this page on GitHub'
    },

    socialLinks: [{
      icon: 'github',
      link: repo,
      ariaLabel: 'View agent-marketplace on GitHub'
    }]
  }
})
