import { defineConfigWithTheme, type DefaultTheme } from 'vitepress'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'
import { DOCS_PATH, GITHUB_REPOSITORY_URL, MARKETPLACE_PATH } from '../src/shared/site'
import { docsLocation, searchVersionFilter } from '../src/shared/docs-versions.mjs'
import { prepareSite } from '../scripts/docs.mjs'
import previewSidebar from './sidebar.mjs'

// The hosting target is deliberately undecided. SITE_BASE lets a project-path
// deployment (e.g. GitHub Pages at /agent-marketplace/) be selected at build
// time without touching any link in the content.
const base = process.env.SITE_BASE ?? '/'

const repo = GITHUB_REPOSITORY_URL
const site = fileURLToPath(new URL('../', import.meta.url))
const docs = prepareSite(site, previewSidebar)

export default defineConfigWithTheme<DefaultTheme.Config & { docsVersions: typeof docs.versions }>({
  base,
  srcDir: docs.srcDir,
  lang: 'en-US',
  title: 'agent-marketplace',
  description:
    'A package manager for AI agent tooling. Distribute skills, commands, subagents and MCP servers from git registries into the native layout of every AI coding harness you use.',
  cleanUrls: true,
  appearance: true,

  vite: {
    plugins: [{
      name: 'docs-source-watcher',
      configureServer(server) {
        const sources = ['docs', 'versions', 'index.md'].map((path) => resolve(site, path))
        server.watcher.add(sources)
        let restart: ReturnType<typeof setTimeout>
        server.watcher.on('all', (_event, path) => {
          if (!sources.some((source) => path === source || path.startsWith(`${source}/`))) return
          clearTimeout(restart)
          restart = setTimeout(() => { server.watcher.emit('change', resolve(site, '.vitepress/config.mts')) }, 100)
        })
        server.httpServer?.once('close', () => clearTimeout(restart))
      }
    }],
    server: {
      // Build output lives inside the site directory, so exclude it from the
      // dev watcher: otherwise a production build triggers a reload per page.
      watch: { ignored: ['**/.vitepress/dist/**', '**/.vitepress/cache/**'] }
    }
  },

  markdown: {
    config(md) {
      md.core.ruler.after('inline', 'versioned-docs-links', (state) => {
        const location = docsLocation(`/${state.env.relativePath}`, docs.versions)
        const version = docs.versions.find((entry) => entry.version === location?.version)
        if (!version) return
        for (const block of state.tokens) {
          for (const token of block.children ?? []) {
            const attribute = token.type === 'link_open' ? 'href' : token.type === 'image' ? 'src' : null
            const href = attribute && token.attrGet(attribute)
            if (attribute && href?.startsWith(DOCS_PATH)) {
              const target = docsLocation(href, docs.versions)
              if (target?.version === 'latest') token.attrSet(attribute, `${version.path}${href.slice(DOCS_PATH.length)}`)
            }
          }
        }
      })
    }
  },

  head: [
    ['link', { rel: 'prefetch', href: MARKETPLACE_PATH }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'agent-marketplace' }],
    ['meta', { name: 'twitter:card', content: 'summary_large_image' }]
  ],

  transformPageData(page) {
    page.frontmatter.sourceUrl = `${repo}/edit/main/site/${page.relativePath}`
    const location = docsLocation(`/${page.relativePath}`, docs.versions)
    if (!location) return
    const version = docs.versions.find((entry) => entry.version === location.version)!
    page.frontmatter.sourceUrl = version.version === 'preview'
      ? `${repo}/edit/main/site/docs/${location.page}.md`
      : `${repo}/blob/${version.source.commit}/${version.source.path}/${location.page}.md`
    page.frontmatter.docsVersion = version.version
    if (version.version === 'preview') {
      page.frontmatter.head ??= []
      page.frontmatter.head.push(['meta', { name: 'robots', content: 'noindex' }])
    }
  },

  themeConfig: {
    siteTitle: 'agent-marketplace',
    docsVersions: docs.versions,

    nav: [
      // The Marketplace is a sibling app at the shared origin root, outside
      // SITE_BASE. Nav `link` values pass through VitePress's withBase/relative
      // normalization, which misresolves from nested docs pages or a non-root
      // base, so a component item renders the origin-root anchor directly.
      { component: 'MarketplaceNavLink' },
      { text: 'Docs', link: DOCS_PATH, activeMatch: `^${DOCS_PATH}` },
      { component: 'DocsVersionSelect' }
    ],

    sidebar: docs.sidebar as DefaultTheme.Sidebar,

    search: {
      provider: 'local',
      options: { miniSearch: { searchOptions: { filter: searchVersionFilter } } }
    },

    editLink: {
      pattern(page) { return page.frontmatter.sourceUrl },
      text: 'View page source on GitHub'
    },

    socialLinks: [{
      icon: 'github',
      link: repo,
      ariaLabel: 'View agent-marketplace on GitHub'
    }]
  }
})
