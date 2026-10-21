import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { nextTabIndex } from '../src/pages/landing/composables/tabNavigation.ts'

const dist = new URL('../.vitepress/dist/', import.meta.url)
const base = process.env.SITE_BASE ?? '/'
const docsPath = `${base.replace(/\/?$/, '/')}docs/`

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
