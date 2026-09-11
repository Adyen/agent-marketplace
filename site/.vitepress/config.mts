import { defineConfig } from 'vitepress'
import { GITHUB_REPOSITORY_URL, MARKETPLACE_PATH } from '../src/shared/site'

// The hosting target is deliberately undecided. SITE_BASE lets a project-path
// deployment (e.g. GitHub Pages at /agent-marketplace/) be selected at build
// time without touching any link in the content.
const base = process.env.SITE_BASE ?? '/'

const repo = GITHUB_REPOSITORY_URL

export default defineConfig({
  base,
  lang: 'en-US',
  title: 'agent-marketplace',
  description:
    'A package manager for AI agent tooling. Distribute skills, commands, subagents and MCP servers from git registries into the native layout of every AI coding harness you use.',
  cleanUrls: true,
  appearance: true,

  vite: {
    server: {
      // Build output lives inside the site directory, so exclude it from the
      // dev watcher: otherwise a production build triggers a reload per page.
      watch: { ignored: ['**/.vitepress/dist/**', '**/.vitepress/cache/**'] }
    }
  },

  head: [
    ['link', { rel: 'prefetch', href: MARKETPLACE_PATH }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'agent-marketplace — built at Adyen' }],
    ['meta', { name: 'twitter:card', content: 'summary_large_image' }]
  ],

  themeConfig: {
    siteTitle: 'agent-marketplace',

    nav: [
      // The Marketplace is always served at /marketplace/ under the shared origin.
      // target keeps VitePress from handling the sibling app as an SPA route.
      { text: 'Marketplace', link: MARKETPLACE_PATH, target: '_self' },
      { text: 'Docs', link: '/#install' }
    ],

    socialLinks: [{
      icon: 'github',
      link: repo,
      ariaLabel: 'View agent-marketplace on GitHub'
    }]
  }
})
