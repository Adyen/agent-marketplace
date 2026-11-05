import assert from 'node:assert/strict'
import { access, mkdir, mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, relative, sep } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import test from 'node:test'

import { stripFrontMatter } from '../.vitepress/llms.ts'
import { docsSidebar } from '../.vitepress/sidebar.ts'
import { nextTabIndex } from '../src/pages/landing/composables/tabNavigation.ts'

const dist = new URL('../.vitepress/dist/', import.meta.url)
const base = process.env.SITE_BASE ?? '/'
const docsPath = `${base.replace(/\/?$/, '/')}docs/`

type SidebarItem = ReturnType<typeof docsSidebar>[number]

function sidebarLinks(items: SidebarItem[]): string[] {
  return items.flatMap(item => [...(item.link ? [item.link] : []), ...sidebarLinks(item.items ?? [])])
}

async function generatedFilePaths(directory: URL): Promise<string[]> {
  const entries = await readdir(directory, { recursive: true, withFileTypes: true })
  return entries
    .filter(entry => entry.isFile())
    .map(entry => relative(fileURLToPath(directory), join(entry.parentPath, entry.name)).split(sep).join('/'))
}

function landingSection(html: string, id: string): string {
  const start = html.indexOf(`id="${id}"`)
  assert.ok(start >= 0, `missing landing section ${id}`)
  return html.slice(start, html.indexOf('</section>', start))
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

// spec: marketplace-web-ui/hero-shows-the-terminal-preview-by-default
// spec: marketplace-web-ui/web-tab-shows-the-catalog-with-a-matching-caption
test('public landing connects both hero preview tabs to their panels', async () => {
  const landing = await readFile(new URL('index.html', dist), 'utf8')
  assert.match(landing, /role="tablist"[^>]*aria-label="Hero preview"/)

  for (const tabName of ['terminal', 'web']) {
    const tabID = `hero-tab-${tabName}`
    const panelID = `hero-panel-${tabName}`
    const tab = landing.match(new RegExp(`<button\\b(?=[^>]*\\bid="${tabID}")[^>]*>`))?.[0]
    const panel = landing.match(new RegExp(`<[^>]+(?=[^>]*\\bid="${panelID}")[^>]*>`))?.[0]

    assert.ok(tab, `missing ${tabID}`)
    assert.match(tab, /role="tab"/)
    assert.match(tab, new RegExp(`aria-controls="${panelID}"`))
    assert.ok(panel, `missing ${panelID}`)
    assert.match(panel, /role="tabpanel"/)
    assert.match(panel, new RegExp(`aria-labelledby="${tabID}"`))
  }

  const terminalTab = landing.match(/<button\b(?=[^>]*\bid="hero-tab-terminal")[^>]*>/)?.[0]
  assert.ok(terminalTab)
  assert.match(terminalTab, /aria-selected="true"/)
  assert.match(terminalTab, /tabindex="0"/)
})

// spec: marketplace-web-ui/browser-frame-shows-catalog-and-resource-detail
test('organizations section depicts the real catalog and a pinned clean resource detail', async () => {
  const landing = await readFile(new URL('index.html', dist), 'utf8')
  const section = landingSection(landing, 'organizations')

  assert.match(section, /class="marketplace-browser-frame"/)
  assert.match(section, /marketplace\.example\.com/)
  assert.match(section, /alt="Marketplace catalog showing skills, commands, and MCP servers with Kind and Registry filters and the Copy install command action\."/)
  assert.match(section, /alt="Marketplace resource detail showing a pinned revision digest and a clean Security result\."/)
})

// spec: marketplace-web-ui/browser-frame-avoids-linking-a-live-marketplace
test('organizations browser frame uses a neutral host and no Marketplace route', async () => {
  const landing = await readFile(new URL('index.html', dist), 'utf8')
  const section = landingSection(landing, 'organizations')

  assert.match(section, /marketplace\.example\.com/)
  assert.doesNotMatch(section, /(?:href|src)="[^"]*\/marketplace\/?"/)
  assert.doesNotMatch(section, /<(?:a|iframe)\b[^>]*\/marketplace\//)
})

// spec: marketplace-web-ui/benefit-cards-and-deployment-guide-remain
test('organizations keeps its three benefits and Deployment guide destination', async () => {
  const landing = await readFile(new URL('index.html', dist), 'utf8')
  const section = landingSection(landing, 'organizations')

  for (const heading of ['Your registries, together', 'Publication policies', 'Your infrastructure']) {
    assert.ok(section.includes(heading), `missing benefit heading: ${heading}`)
  }
  assert.match(section, /href="\/docs\/how-to\/operator\/deploy-a-production-marketplace"[^>]*>[\s\S]*?Deployment guide/)
})

// spec: marketplace-web-ui/depictions-declare-dimensions-alt-text-and-lazy-loading
test('below-the-fold web UI images declare dimensions, meaningful alt text, and lazy loading', async () => {
  const landing = await readFile(new URL('index.html', dist), 'utf8')
  const images = [...landing.matchAll(/<img\b[^>]*data-web-ui-image[^>]*>/g)].map(([tag]) => tag)

  assert.equal(images.length, 4)
  for (const image of images) {
    assert.match(image, /\bwidth="1440"/)
    assert.match(image, /\bheight="960"/)
    assert.match(image, /\balt="[^"]{20,}"/)
    assert.match(image, /\bloading="lazy"/)
  }
  assert.equal(images.filter(image => image.includes('data-theme="light"')).length, 2)
  assert.equal(images.filter(image => image.includes('data-theme="dark"')).length, 2)
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

const sourceDocs = new URL('../docs/', import.meta.url)

async function sourceMarkdown(directory: URL, prefix = ''): Promise<string[]> {
  const entries = await readdir(directory, { withFileTypes: true })
  const files = await Promise.all(entries.map(entry => {
    if (entry.isDirectory()) {
      return entry.name === 'images' ? [] : sourceMarkdown(new URL(`${entry.name}/`, directory), `${prefix}${entry.name}/`)
    }
    return entry.name.endsWith('.md') ? [`${prefix}${entry.name}`] : []
  }))
  return files.flat()
}

// spec: site-deployment-profiles/llm-index-lists-every-documentation-page
test('llms.txt lists every documentation page as a Markdown link under the base', async () => {
  const llms = await readFile(new URL('llms.txt', dist), 'utf8')
  assert.match(llms, /^# agent-marketplace\n\n> .+\n/)

  const links = [...llms.matchAll(/\]\(([^)]+)\)/g)].map(([, url]) => url)
  for (const url of links) {
    assert.ok(url.startsWith(docsPath) && url.endsWith('.md'), url)
    await access(new URL(url.slice(base.length), dist))
  }
  const expectedOrdered = sidebarLinks(docsSidebar(docsPath)).map(link => {
    const rest = link.slice(docsPath.length)
    const rel = rest === '' || rest.endsWith('/') ? `${rest}index` : rest
    return `${docsPath}${rel}.md`
  })
  assert.deepEqual(links, expectedOrdered)
  const expected = (await sourceMarkdown(sourceDocs)).map(rel => `${docsPath}${rel}`).sort()
  assert.deepEqual([...links].sort(), expected)
})

test('front matter is stripped from LF and CRLF sources', () => {
  assert.equal(stripFrontMatter('---\ntitle: x\n---\n# Body\n'), '# Body\n')
  assert.equal(stripFrontMatter('---\r\ntitle: x\r\n---\r\n# Body\r\n'), '# Body\r\n')
  assert.equal(stripFrontMatter('---\n---\n# Body\n'), '# Body\n')
  assert.equal(stripFrontMatter('---\r\n---\r\n# Body\r\n'), '# Body\r\n')
  assert.equal(stripFrontMatter('# Body\n'), '# Body\n')
})

// spec: site-deployment-profiles/markdown-copies-mirror-documentation-sources
test('Markdown copies mirror their sources without front matter', async () => {
  for (const rel of await sourceMarkdown(sourceDocs)) {
    const [source, copy] = await Promise.all([
      readFile(new URL(rel, sourceDocs), 'utf8'),
      readFile(new URL(`docs/${rel}`, dist), 'utf8'),
    ])
    assert.equal(copy, stripFrontMatter(source), rel)
  }

  const metadata = await readFile(new URL('docs/reference/resource-metadata.md', dist), 'utf8')
  assert.doesNotMatch(metadata, /^---\r?\n/)

  const tutorialUrl = new URL('docs/tutorials/add-your-first-marketplace-registry.md', dist)
  const tutorial = await readFile(tutorialUrl, 'utf8')
  const images = [...tutorial.matchAll(/!\[[^\]]*\]\(([^)]+)\)/g)].map(([, target]) => target)
  assert.ok(images.length > 0)
  for (const target of images) await access(new URL(target, tutorialUrl))
})
