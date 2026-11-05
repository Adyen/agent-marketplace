import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import { relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const publicDist = new URL('../.vitepress/dist/', import.meta.url)
const publicDocsDist = new URL('docs/', publicDist)
const docsDist = new URL('../.vitepress/dist-docs/', import.meta.url)
const publicBase = process.env.SITE_BASE ?? '/'
const marketplaceBase = process.env.MARKETPLACE_BASE ?? '/'
const marketplaceBasePattern = marketplaceBase.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
const docsOrigin = new URL('https://docs-profile.invalid/docs/')
const marketplaceURL = new URL(marketplaceBase, docsOrigin)

async function filesBelow(directory: URL): Promise<URL[]> {
  const entries = await readdir(directory, { withFileTypes: true })
  const files = await Promise.all(entries.map(entry => {
    const child = new URL(entry.name + (entry.isDirectory() ? '/' : ''), directory)
    return entry.isDirectory() ? filesBelow(child) : [child]
  }))
  return files.flat()
}

// spec: site-deployment-profiles/documentation-build-is-self-contained-below-docs
test('documentation-only build is contained below /docs/', async () => {
  const files = await filesBelow(docsDist)
  const htmlFiles = files.filter(file => file.pathname.endsWith('.html'))
  const index = await readFile(new URL('index.html', docsDist), 'utf8')

  assert.match(index, /Documentation/)
  assert.match(index, /<a[^>]+href="\/docs\/"[^>]+target="_self"[^>]*>Docs<\/a>/)

  assert.doesNotMatch(index, /Manage AI agent tooling once/)
  assert.ok(htmlFiles.length > 1)

  for (const file of htmlFiles) {
    const html = await readFile(file, 'utf8')
    const pageURL = new URL(relative(fileURLToPath(docsDist), fileURLToPath(file)), docsOrigin)
    for (const [, target] of html.matchAll(/(?:href|src)="([^"]+)"/g)) {
      const resolved = new URL(target, pageURL)
      if (resolved.origin !== docsOrigin.origin || resolved.pathname === marketplaceURL.pathname) continue
      assert.ok(resolved.pathname.startsWith('/docs/'), `${file.pathname} escapes /docs/: ${target}`)
    }
  }
})

function htmlInventory(root: URL, files: URL[]): string[] {
  const rootPath = fileURLToPath(root)
  return files
    .filter(file => file.pathname.endsWith('.html') && !file.pathname.endsWith('/404.html'))
    .map(file => relative(rootPath, fileURLToPath(file)))
    .sort()
}

function articleContent(html: string): string {
  const match = html.match(/<main\b[^>]*>([\s\S]*?)<\/main>/)
  assert.ok(match, 'rendered documentation page has no main content')
  return match[1]
    .replace(/class="vp-doc [^"]+"/g, 'class="vp-doc"')
    .replace(/\sdata-v-[\da-f]+(?:="")?/g, '')
    .replace(/src="\/(?:[^"/]+\/)*assets\//g, 'src="/assets/')
}

// spec: site-deployment-profiles/navigation-respects-deployment-context
test('documentation navigation follows its deployment profile', async () => {
  const [publicDocs, marketplaceDocs] = await Promise.all([
    readFile(new URL('index.html', publicDocsDist), 'utf8'),
    readFile(new URL('index.html', docsDist), 'utf8'),
  ])

  assert.ok(publicDocs.includes(`class="title" href="${publicBase}"`))
  assert.doesNotMatch(publicDocs, />Marketplace<\/a>/)
  assert.match(marketplaceDocs, new RegExp(`<a[^>]+href="${marketplaceBasePattern}"[^>]+target="_self"[^>]*>Marketplace</a>`))
  assert.match(marketplaceDocs, /<a[^>]+href="\/docs\/"[^>]+target="_self"[^>]*>Docs<\/a>/)
})

// spec: site-deployment-profiles/both-profiles-publish-the-same-documentation
test('public and documentation-only builds publish the same documentation', async () => {
  const [publicFiles, docsFiles] = await Promise.all([
    filesBelow(publicDocsDist),
    filesBelow(docsDist),
  ])
  const publicInventory = htmlInventory(publicDocsDist, publicFiles)
  const docsInventory = htmlInventory(docsDist, docsFiles)

  assert.deepEqual(docsInventory, publicInventory)

  for (const route of publicInventory) {
    const [publicPage, docsPage] = await Promise.all([
      readFile(new URL(route, publicDocsDist), 'utf8'),
      readFile(new URL(route, docsDist), 'utf8'),
    ])
    assert.equal(articleContent(docsPage), articleContent(publicPage), route)
  }
})

function markdownInventory(root: URL, files: URL[]): string[] {
  const rootPath = fileURLToPath(root)
  return files
    .filter(file => file.pathname.endsWith('.md'))
    .map(file => relative(rootPath, fileURLToPath(file)))
    .sort()
}

// spec: site-deployment-profiles/both-profiles-publish-the-same-markdown-copies
test('public and documentation-only builds publish the same Markdown copies and llms index', async () => {
  const [publicFiles, docsFiles] = await Promise.all([
    filesBelow(publicDocsDist),
    filesBelow(docsDist),
  ])
  const publicInventory = markdownInventory(publicDocsDist, publicFiles)
  assert.deepEqual(markdownInventory(docsDist, docsFiles), publicInventory)

  for (const file of publicInventory) {
    const [publicCopy, docsCopy] = await Promise.all([
      readFile(new URL(file, publicDocsDist), 'utf8'),
      readFile(new URL(file, docsDist), 'utf8'),
    ])
    assert.equal(docsCopy, publicCopy, file)
  }

  const [publicIndex, docsIndex] = await Promise.all([
    readFile(new URL('llms.txt', publicDist), 'utf8'),
    readFile(new URL('llms.txt', docsDist), 'utf8'),
  ])
  const links = [...docsIndex.matchAll(/\]\(([^)]+)\)/g)].map(([, url]) => url)
  assert.ok(links.length > 0 && links.every(url => url.startsWith('/docs/')))
  assert.equal(docsIndex, publicIndex.replaceAll(`${publicBase}docs/`, '/docs/'))
})
