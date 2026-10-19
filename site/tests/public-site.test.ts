import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const dist = new URL('../.vitepress/dist/', import.meta.url)
const base = process.env.SITE_BASE ?? '/'
const docsPath = `${base.replace(/\/?$/, '/')}docs/`

// spec: install-script/site-build-publishes-the-install-script
test('public site publishes the install script unchanged and shows its command', async () => {
  const [source, built, landing] = await Promise.all([
    readFile(new URL('../public/install.sh', import.meta.url)),
    readFile(new URL('install.sh', dist)),
    readFile(new URL('index.html', dist), 'utf8'),
  ])

  assert.deepEqual(built, source)
  assert.ok(landing.includes('curl -fsSL https://agent-marketplace.io/install.sh | bash'))
})

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

  for (const html of [landing, nestedDocs]) {
    assert.doesNotMatch(html, /(?:href|rel="prefetch" href)="\/marketplace\/?"/)
  }
})
