import assert from 'node:assert/strict'
import { mkdir, mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, relative, sep } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import test from 'node:test'

import { nextTabIndex } from '../src/pages/landing/composables/tabNavigation.ts'

const dist = new URL('../.vitepress/dist/', import.meta.url)
const base = process.env.SITE_BASE ?? '/'
const docsPath = `${base.replace(/\/?$/, '/')}docs/`

async function generatedFilePaths(directory: URL): Promise<string[]> {
  const entries = await readdir(directory, { recursive: true, withFileTypes: true })
  return entries
    .filter(entry => entry.isFile())
    .map(entry => relative(fileURLToPath(directory), join(entry.parentPath, entry.name)).split(sep).join('/'))
}

// spec: install-script/site-build-publishes-the-install-script
test('public site publishes the install script byte-for-byte', async () => {
  const [source, built] = await Promise.all([
    readFile(new URL('../public/install.sh', import.meta.url)),
    readFile(new URL('install.sh', dist)),
  ])

  assert.deepEqual(built, source)
})

// spec: marketplace-web-ui/default-harness-leads-the-tab-order
// spec: site-deployment-profiles/public-project-site-exposes-only-landing-and-documentation
test('public project site exposes landing and documentation without a local Marketplace', async () => {
  const [landing, docs, nestedDocs] = await Promise.all([
    readFile(new URL('index.html', dist), 'utf8'),
    readFile(new URL('docs/index.html', dist), 'utf8'),
    readFile(new URL('docs/reference/cli.html', dist), 'utf8'),
  ])

  assert.match(landing, /Manage AI agent tooling once/)
  assert.match(docs, /Documentation/)
  assert.ok(landing.includes(`href="${docsPath}"`))
  assert.match(landing, /href="https:\/\/github\.com\/Adyen\/agent-marketplace"/)
  assert.match(landing, /class="am-site"/)
  assert.match(docs, /class="am-site"/)
  assert.match(landing, /data-section-edge="top bottom"/)
  assert.match(landing, /data-section-edge="top"/)
  assert.match(landing, /data-section-edge="bottom"/)
  assert.ok(
    landing.indexOf('Claude Code') < landing.indexOf('Factory Droid'),
    'the first harness tab is the intentional default',
  )

  for (const html of [landing, nestedDocs]) {
    assert.doesNotMatch(html, /(?:href|rel="prefetch" href)="\/marketplace\/?"/)
  }
})

// spec: site-deployment-profiles/project-path-pages-site-resolves-navigation-and-assets
// spec: site-deployment-profiles/custom-domain-pages-site-resolves-navigation-and-assets
test('public navigation and assets resolve within the deployment base', async () => {
  const files = await generatedFilePaths(dist)
  const generated = new Set(files)
  const origin = new URL(`https://pages.invalid${base}`)

  for (const file of files.filter(file => file.endsWith('.html'))) {
    const html = await readFile(new URL(file, dist), 'utf8')
    const page = new URL(file, origin)
    for (const [, target] of html.matchAll(/(?:href|src)="([^"]+)"/g)) {
      const resolved = new URL(target, page)
      if (resolved.origin !== origin.origin) continue
      assert.ok(resolved.pathname.startsWith(base), `${file} escapes ${base}: ${target}`)
      const path = decodeURIComponent(resolved.pathname.slice(base.length))
      const candidates = path === '' || path.endsWith('/') ? [`${path}index.html`] : [path, `${path}.html`, `${path}/index.html`]
      assert.ok(candidates.some(candidate => generated.has(candidate)), `${file} has a missing local target: ${target}`)
    }
  }
})

test('generated targets contain only files with URL path separators', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'public-site-targets-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  await mkdir(join(directory, 'guide'))
  await mkdir(join(directory, 'docs'))
  await writeFile(join(directory, 'docs', 'index.html'), 'documentation')

  assert.deepEqual(await generatedFilePaths(pathToFileURL(directory)), ['docs/index.html'])
})

// spec: marketplace-web-ui/harness-tabs-wrap-keyboard-selection
test('landing tabs use one wrapping keyboard-navigation rule', () => {
  assert.equal(nextTabIndex('ArrowRight', 2, 3), 0)
  assert.equal(nextTabIndex('ArrowLeft', 0, 3), 2)
  assert.equal(nextTabIndex('ArrowDown', 0, 3), 1)
  assert.equal(nextTabIndex('ArrowUp', 1, 3), 0)
  assert.equal(nextTabIndex('Home', 2, 3), 0)
  assert.equal(nextTabIndex('End', 0, 3), 2)
  assert.equal(nextTabIndex('Tab', 1, 3), null)
  assert.equal(nextTabIndex('ArrowRight', 0, 0), null)
})
