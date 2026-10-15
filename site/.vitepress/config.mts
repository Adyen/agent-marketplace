import { defineConfig } from 'vitepress'
import { DOCS_PATH as PUBLIC_DOCS_PATH, GITHUB_REPOSITORY_URL } from '../src/shared/site'

const docsOnly = process.env.SITE_PROFILE === 'docs'

function deploymentBase(name: string, value: string): string {
  if (!value.startsWith('/') || !value.endsWith('/') || value.startsWith('//') || value.includes('\\') || /[?#\r\n]/.test(value) || value.split('/').some(part => part === '.' || part === '..')) {
    throw new Error(`${name} must be a canonical absolute path ending in /`)
  }
  return value
}

// The hosting target is deliberately undecided. SITE_BASE lets a project-path
// deployment (e.g. GitHub Pages at /agent-marketplace/) be selected at build
// time without touching any link in the content.
const base = docsOnly ? '/docs/' : deploymentBase('SITE_BASE', process.env.SITE_BASE ?? '/')
const marketplaceBase = docsOnly ? deploymentBase('MARKETPLACE_BASE', process.env.MARKETPLACE_BASE ?? '/') : '/'
const DOCS_PATH = docsOnly ? '/' : PUBLIC_DOCS_PATH
const publicAsset = (path: string) => `${base}${path}`

const repo = GITHUB_REPOSITORY_URL

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

    // One entry per page, ordered by hand. Add a line here when adding a page
    // under site/docs/.
    sidebar: {
      [DOCS_PATH]: [
        { text: 'Overview', link: DOCS_PATH },
        { text: 'Terminal basics', link: `${DOCS_PATH}terminal-basics` },
        { text: 'Install', link: `${DOCS_PATH}install` },
        { text: 'Quick start', link: `${DOCS_PATH}quick-start` },
        { text: 'Glossary', link: `${DOCS_PATH}glossary` },
        {
          text: 'Tutorials',
          items: [
            {
              text: 'Consumers',
              items: [
                { text: 'Install your first skill', link: `${DOCS_PATH}tutorials/install-your-first-skill` },
                { text: 'Install your first Marketplace skill', link: `${DOCS_PATH}tutorials/install-your-first-marketplace-skill` }
              ]
            },
            {
              text: 'Registry authors',
              items: [
                { text: 'Create your first registry', link: `${DOCS_PATH}tutorials/create-your-first-registry` }
              ]
            },
            {
              text: 'Marketplace operators',
              items: [
                { text: 'Run a local Marketplace', link: `${DOCS_PATH}tutorials/run-a-local-marketplace` },
                { text: 'Add your first registry', link: `${DOCS_PATH}tutorials/add-your-first-marketplace-registry` }
              ]
            }
          ]
        },
        {
          text: 'How-to guides',
          items: [
            {
              text: 'Consumers',
              items: [
                { text: 'Browse and inspect Marketplace resources', link: `${DOCS_PATH}how-to/browse-marketplace-resources` },
                { text: 'Adopt in a repository', link: `${DOCS_PATH}how-to/adopt-agent-marketplace-in-a-repository` },
                { text: 'Keep resources reproducible', link: `${DOCS_PATH}how-to/keep-resources-reproducible` },
                { text: 'Troubleshoot sync drift', link: `${DOCS_PATH}how-to/troubleshoot-sync-drift` },
                { text: 'Install resource bundles', link: `${DOCS_PATH}how-to/install-resource-bundles` },
                { text: 'Remove or override resources', link: `${DOCS_PATH}how-to/remove-or-override-resources` },
                { text: 'Use personal repository overrides', link: `${DOCS_PATH}how-to/use-personal-repository-overrides` },
                { text: 'Configure multiple harnesses', link: `${DOCS_PATH}how-to/configure-multiple-harnesses` },
                { text: 'Write regular files instead of symlinks', link: `${DOCS_PATH}how-to/write-regular-files-instead-of-symlinks` },
                { text: 'Use a local registry', link: `${DOCS_PATH}how-to/use-a-local-registry` },
                { text: 'Convert configuration before upgrading', link: `${DOCS_PATH}how-to/migrate-configuration` },
                { text: 'Convert a harness layout', link: `${DOCS_PATH}how-to/convert-a-harness-layout` },
                { text: 'Repair owned shared configuration', link: `${DOCS_PATH}how-to/repair-owned-shared-configuration` },
                { text: 'Back up and restore machine resources', link: `${DOCS_PATH}how-to/back-up-and-restore-machine-resources` },
                { text: 'Remove agent-marketplace safely', link: `${DOCS_PATH}how-to/remove-agent-marketplace-safely` },
                { text: 'Clean the cache safely', link: `${DOCS_PATH}how-to/clean-the-cache-safely` },
                { text: 'Configure private Git registries', link: `${DOCS_PATH}how-to/configure-private-git-registries` },
                { text: 'Configure telemetry', link: `${DOCS_PATH}how-to/configure-telemetry` },
                { text: 'Install from a hosted Marketplace', link: `${DOCS_PATH}how-to/install-from-a-marketplace` }
              ]
            },
            {
              text: 'Registry authors',
              items: [
                { text: 'Add a resource to a registry', link: `${DOCS_PATH}how-to/author/add-a-resource-to-a-registry` },
                { text: 'Publish resource collections', link: `${DOCS_PATH}how-to/author/publish-resource-collections` },
                { text: 'Declare tags and a license', link: `${DOCS_PATH}how-to/author/declare-tags-and-licenses` },
                { text: 'Declare resource prerequisites', link: `${DOCS_PATH}how-to/author/declare-resource-prerequisites` },
                { text: 'Validate a registry in CI', link: `${DOCS_PATH}how-to/author/validate-a-registry-in-ci` },
                { text: 'Version and release a registry', link: `${DOCS_PATH}how-to/author/version-and-release-a-registry` }
              ]
            },
            {
              text: 'Marketplace operators',
              items: [
                { text: 'Deploy a production Marketplace', link: `${DOCS_PATH}how-to/operator/deploy-a-production-marketplace` },
                { text: 'Enable and use the admin panel', link: `${DOCS_PATH}how-to/operator/enable-and-use-the-admin-panel` },
                { text: 'Test locally with Compose', link: `${DOCS_PATH}how-to/operator/test-locally-with-compose` },
                { text: 'Configure a reverse proxy', link: `${DOCS_PATH}how-to/operator/configure-a-reverse-proxy` },
                { text: 'Connect a private registry', link: `${DOCS_PATH}how-to/operator/connect-a-private-registry` },
                { text: 'Back up and restore a Marketplace', link: `${DOCS_PATH}how-to/operator/back-up-and-restore` },
                { text: 'Manage ingestion policy', link: `${DOCS_PATH}how-to/operator/manage-ingestion-policy` },
                { text: 'Revoke a resource', link: `${DOCS_PATH}how-to/operator/revoke-a-resource` },
                { text: 'Reindex and monitor', link: `${DOCS_PATH}how-to/operator/reindex-and-monitor` },
                { text: 'Reset and reindex a registry', link: `${DOCS_PATH}how-to/operator/reset-and-reindex-a-registry` }
              ]
            }
          ]
        },
        {
          text: 'Reference',
          items: [
            { text: 'Command line', link: `${DOCS_PATH}reference/cli` },
            { text: 'Repository configuration', link: `${DOCS_PATH}reference/configuration` },
            { text: 'Configuration layers', link: `${DOCS_PATH}reference/configuration-layers` },
            { text: 'Harnesses and output paths', link: `${DOCS_PATH}reference/harnesses-and-output-paths` },
            { text: 'TUI', link: `${DOCS_PATH}reference/tui` },
            { text: 'Lockfiles', link: `${DOCS_PATH}reference/lockfiles` },
            { text: 'Filesystem layout', link: `${DOCS_PATH}reference/filesystem-layout` },
            { text: 'Environment variables', link: `${DOCS_PATH}reference/environment-variables` },
            { text: 'Diagnostics', link: `${DOCS_PATH}reference/diagnostics` },
            { text: 'Logging', link: `${DOCS_PATH}reference/logging` },
            { text: 'Telemetry', link: `${DOCS_PATH}reference/telemetry` },
            { text: 'Marketplace operator configuration', link: `${DOCS_PATH}reference/marketplace-operator-configuration` },
            { text: 'Marketplace HTTP API', link: `${DOCS_PATH}reference/marketplace-http-api` },
            { text: 'Marketplace health and metrics', link: `${DOCS_PATH}reference/marketplace-health-and-metrics` },
            { text: 'Marketplace resource locators', link: `${DOCS_PATH}reference/marketplace-resource-locators` },
            { text: 'Registry layout', link: `${DOCS_PATH}reference/registry-layout` },
            { text: 'Resource metadata', link: `${DOCS_PATH}reference/resource-metadata` },
            { text: 'Resource prerequisites', link: `${DOCS_PATH}reference/resource-prerequisites` },
            { text: 'Collections', link: `${DOCS_PATH}reference/collections` }
          ]
        },
        {
          text: 'Explanation',
          items: [
            { text: 'User journeys', link: `${DOCS_PATH}explanation/user-journeys` },
            { text: 'How agent-marketplace works', link: `${DOCS_PATH}explanation/how-agent-marketplace-works` },
            { text: 'Sources and resources', link: `${DOCS_PATH}explanation/sources-and-resources` },
            { text: 'Scopes, selection, and precedence', link: `${DOCS_PATH}explanation/scopes-selection-and-precedence` },
            { text: 'Native harness materialization', link: `${DOCS_PATH}explanation/native-harness-materialization` },
            { text: 'Reproducibility and lockfiles', link: `${DOCS_PATH}explanation/reproducibility-and-lockfiles` },
            { text: 'Shared configuration ownership', link: `${DOCS_PATH}explanation/shared-configuration-ownership` },
            { text: 'Git registries and hosted Marketplaces', link: `${DOCS_PATH}explanation/git-registries-and-hosted-marketplaces` },
            { text: 'Telemetry and privacy', link: `${DOCS_PATH}explanation/telemetry-and-privacy` },
            { text: 'Marketplace architecture', link: `${DOCS_PATH}explanation/marketplace-architecture` },
            { text: 'Marketplace trust model', link: `${DOCS_PATH}explanation/marketplace-trust-model` }
          ]
        }
      ]
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
