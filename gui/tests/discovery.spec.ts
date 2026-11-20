import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { existsSync } from 'node:fs'
import { mkdir, readFile, unlink, writeFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { startHarness } from './harness'
import { distIndexExists, launchBuiltUserApp } from './app'
import { focusedDescription, tabTo } from './keyboard'

async function saveActions(testInfo: TestInfo, actions: string[]): Promise<void> {
  const path = testInfo.outputPath('discovery.actions.json')
  await mkdir(dirname(path), { recursive: true })
  await writeFile(path, `${JSON.stringify({ actions }, null, 2)}\n`)
}

async function waitForMarker(path: string | undefined, timeout = 30_000): Promise<void> {
  if (path == null) {
    throw new Error('discovery harness marker missing')
  }
  await expect.poll(() => existsSync(path), { timeout }).toBe(true)
}

async function markerCount(path: string | undefined): Promise<number> {
  if (path == null) {
    throw new Error('discovery harness counter missing')
  }
  const value = Number((await readFile(path, 'utf8')).trim())
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new Error('discovery harness counter was malformed')
  }
  return value
}

// spec: session-navigation/remote-marketplace-search-is-debounced
// spec: session-startup/config-stage-starts-cached-first-marketplace-discovery-before-local-verification-finishes
// spec: session-startup/cached-marketplace-rows-render-while-acquisition-is-blocked
// spec: session-startup/browse-safe-config-publishes-the-verified-endpoint-before-acquisition
// spec: session-startup/final-stage-merges-discovery-published-during-the-config-stage
test('built GUI consumes cached-first Marketplace discovery across a held trusted config stage', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the discovery journey')
  }

  const actions: string[] = []
  const firstQuery = String.fromCharCode(97, 108, 112, 104, 97)
  const activeQuery = String.fromCharCode(98, 101, 116, 97)
  const outputDir = dirname(testInfo.outputPath('discovery.actions.json'))
  const harnessLogDir = join(outputDir, 'harness')
  await mkdir(harnessLogDir, { recursive: true })
  const harness = await startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'work35-discovery',
    AM_LOCALAPI_RELEASE_VERSION: 'dev',
    AM_WORK28_TELEMETRY_EVIDENCE_DIR: harnessLogDir,
  })
  const { bootstrap } = harness
  let released = false
  try {
    await page.setViewportSize({ width: 1280, height: 800 })
    await launchBuiltUserApp(page, bootstrap)
    const list = page.getByRole('region', { name: 'Resources' })
    const detail = page.getByRole('complementary', { name: 'Detail' })
    const searchButton = page.getByRole('button', { name: 'Search', exact: true })
    const discoveryHealth = page.getByRole('region', { name: 'Marketplace discovery health' })
    let discoveryRequests = 0
    let telemetryRequests = 0
    page.on('request', (request) => {
      const pathname = new URL(request.url()).pathname
      if (pathname === '/api/discovery') {
        discoveryRequests++
      } else if (pathname === '/api/telemetry') {
        telemetryRequests++
      }
    })

    await expect(list).toBeVisible({ timeout: 60_000 })
    await waitForMarker(bootstrap.discoveryAcquisitionStarted)
    actions.push('Trusted organization acquisition entered its held request')
    await saveActions(testInfo, actions)
    const localPending = page.getByRole('option').filter({ hasText: 'solo' })
    const cachedRemote = page.getByRole('option').filter({ hasText: 'cached-resource' })
    await expect(localPending).toBeVisible({ timeout: 10_000 })
    await expect(cachedRemote).toBeVisible({ timeout: 10_000 })
    await expect(detail).toContainText('verification', { timeout: 10_000 })
    await page.getByRole('button', { name: 'Show diagnostics' }).click()
    await expect(page.locator('.gui-diagnostics-view')).toContainText('Drift: pending', { timeout: 10_000 })
    await page.screenshot({ path: testInfo.outputPath('discovery-held-cache.png'), fullPage: true, animations: 'disabled' })
    await expect(discoveryHealth).toContainText('Results are current', { timeout: 10_000 })
    if (bootstrap.marketplaceEndpoint == null) {
      throw new Error('trusted Marketplace endpoint fixture missing')
    }
    await expect(discoveryHealth).toContainText(new URL(bootstrap.marketplaceEndpoint).host)
    if ((await markerCount(bootstrap.discoverySearchCount)) !== 0) {
      throw new Error('fresh cached browse issued a remote discovery search')
    }
    actions.push('Pending local row and fresh cached Marketplace row rendered while held')
    await saveActions(testInfo, actions)
    await page.screenshot({ path: testInfo.outputPath('discovery-held-cache.png'), fullPage: true, animations: 'disabled' })
    await page.getByRole('button', { name: 'Show all groups' }).click()

    const telemetryBefore = telemetryRequests

    const initialFocus = await focusedDescription(page)
    if (initialFocus !== 'button "Search"') {
      const navigationKey = initialFocus === '<none>' || initialFocus === 'body' ? 'Tab' : 'Shift+Tab'
      try {
        await tabTo(page, searchButton, 'Search button', actions, undefined, navigationKey)
      } finally {
        await saveActions(testInfo, actions)
      }
    }
    await page.keyboard.press('Enter')
    const searchInput = page.getByRole('searchbox', { name: 'Search resources' })
    await expect(searchInput).toBeFocused()
    actions.push('Opened the Search control by keyboard')
    await saveActions(testInfo, actions)
    await page.keyboard.type(firstQuery)
    actions.push('Entered a query through the focused search input')
    await saveActions(testInfo, actions)
    await waitForMarker(bootstrap.discoveryFirstSearchStarted)
    await expect.poll(() => markerCount(bootstrap.discoverySearchCount), { timeout: 10_000 }).toBeGreaterThan(0)
    const firstSearchCount = await markerCount(bootstrap.discoverySearchCount)
    actions.push('Typed a query by keyboard; the server reached the remote route after its debounce')
    await saveActions(testInfo, actions)

    await page.keyboard.press('ControlOrMeta+A')
    await page.keyboard.type(activeQuery)
    await waitForMarker(bootstrap.discoveryFirstSearchCanceled)
    await waitForMarker(bootstrap.discoverySecondSearchStarted)
    await expect.poll(() => markerCount(bootstrap.discoverySearchCount), { timeout: 10_000 }).toBeGreaterThan(firstSearchCount)
    const remote = page.getByRole('option').filter({ hasText: 'remote-beta' })
    await expect(remote).toBeVisible({ timeout: 60_000 })
    await expect(remote).toHaveAttribute('aria-selected', 'true')
    await expect(page.locator('.workspace__search-status')).toContainText('0 local matches')
    await page.getByRole('button', { name: 'Show diagnostics' }).click()
    await expect(discoveryHealth).toContainText('Results are current')
    await expect(discoveryHealth).not.toContainText('miss')
    await page.getByRole('button', { name: 'Show all groups' }).click()
    expect(discoveryRequests).toBeGreaterThan(1)
    expect(telemetryRequests).toBe(telemetryBefore)
    await expect(detail).toContainText('remote-beta')
    actions.push('Superseded the held remote request; only the current query result became selected')
    await saveActions(testInfo, actions)
    await page.screenshot({ path: testInfo.outputPath('discovery-current-query.png'), fullPage: true, animations: 'disabled' })

    if (bootstrap.discoveryRelease == null) {
      throw new Error('discovery release marker missing')
    }
    await writeFile(bootstrap.discoveryRelease, 'released\n')
    released = true
    await page.getByRole('button', { name: 'Show diagnostics' }).click()
    await expect.poll(async () => /Drift: (ready|failed)/.test((await page.locator('.gui-diagnostics-view').innerText()) ?? ''), { timeout: 60_000 }).toBe(true)
    await expect(discoveryHealth).toContainText('Marketplace')
    await page.getByRole('button', { name: 'Show all groups' }).click()
    await expect(remote).toBeVisible({ timeout: 60_000 })
    await expect(remote).toHaveAttribute('aria-selected', 'true')
    actions.push('Released startup verification; the authoritative final view retained the current discovery selection and health')
    await saveActions(testInfo, actions)
    await page.screenshot({ path: testInfo.outputPath('discovery-final.png'), fullPage: true, animations: 'disabled' })
  } finally {
    if (!released && bootstrap.discoveryRelease != null) {
      await writeFile(bootstrap.discoveryRelease, 'released\n')
    }
    await harness.stop()
    await saveActions(testInfo, actions)
  }
})

type PagedDiscoveryCase = {
  name: string
  mode: 'complete' | 'failure'
}

const pagedDiscoveryCases: PagedDiscoveryCase[] = [
  { name: 'empty-query discovery receives later Marketplace pages', mode: 'complete' },
  { name: 'failed later Marketplace page leaves empty-query discovery incomplete', mode: 'failure' },
]

async function runPagedDiscoveryCase(page: Page, testInfo: TestInfo, scenario: PagedDiscoveryCase): Promise<void> {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the discovery journey')
  }

  const harnessLogDir = join(dirname(testInfo.outputPath('discovery.actions.json')), 'harness')
  await mkdir(harnessLogDir, { recursive: true })
  const harness = await startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'work41-paged-discovery',
    AM_LOCALAPI_USER_DISCOVERY_MODE: scenario.mode,
    AM_WORK28_TELEMETRY_EVIDENCE_DIR: harnessLogDir,
  })
  const { bootstrap } = harness
  const pageTwoStarted = bootstrap.discoveryPageTwoStarted
  const pageTwoRelease = bootstrap.discoveryPageTwoRelease
  let released = false
  try {
    if (pageTwoStarted == null || pageTwoRelease == null) {
      throw new Error('paged discovery markers missing')
    }
    await page.setViewportSize({ width: 1280, height: 800 })
    await launchBuiltUserApp(page, bootstrap)
    const first = page.getByRole('option').filter({ hasText: 'paged-first' })
    const second = page.getByRole('option').filter({ hasText: 'paged-second' })
    await expect(first).toBeVisible({ timeout: 60_000 })
    await page.screenshot({ path: testInfo.outputPath('discovery-first-page.png'), animations: 'disabled' })

    const expectFirstPageOnly = async () => {
      await expect(first).toBeVisible()
      await expect(second).not.toBeVisible()
    }
    await waitForMarker(pageTwoStarted, 10_000)
    await expectFirstPageOnly()
    await page.screenshot({ path: testInfo.outputPath('discovery-page-two-held.png'), animations: 'disabled' })

    if (scenario.mode === 'complete') {
      await writeFile(pageTwoRelease, 'released\n')
      released = true
      await expect(second).toBeVisible({ timeout: 60_000 })
      await page.screenshot({ path: testInfo.outputPath('discovery-complete.png'), animations: 'disabled' })
      return
    }

    await page.getByRole('button', { name: 'Search', exact: true }).click()
    const search = page.getByRole('searchbox', { name: 'Search resources' })
    await search.fill('paged-search')
    await search.press('Enter')
    const searchResult = page.getByRole('option').filter({ hasText: 'paged-search' })
    await expect(searchResult).toBeVisible({ timeout: 60_000 })
    await page.getByRole('button', { name: 'Show diagnostics' }).click()
    const discoveryHealth = page.getByRole('region', { name: 'Marketplace discovery health' })
    await writeFile(pageTwoRelease, 'released\n')
    released = true
    const expectIncompleteDiscovery = async () => {
      await expect(discoveryHealth).toContainText('Marketplace search failed', { timeout: 60_000 })
      await expect(discoveryHealth).toContainText(
        'Only part of the catalog is shown; search to find more',
        { timeout: 60_000 },
      )
    }
    await expectIncompleteDiscovery()
    await discoveryHealth.screenshot({
      path: testInfo.outputPath('discovery-incomplete.png'),
      animations: 'disabled',
    })

    await unlink(pageTwoStarted)
    await unlink(pageTwoRelease)
    released = false
    await page.getByRole('button', { name: 'Retry Marketplace search' }).click()
    await waitForMarker(pageTwoStarted, 10_000)
    const loading = discoveryHealth.getByText('Marketplace discovery is updating.')
    await expect(loading).toBeVisible()
    await writeFile(pageTwoRelease, 'released\n')
    released = true
    await expect(loading).not.toBeVisible({ timeout: 60_000 })
    await expectIncompleteDiscovery()

    await page.getByRole('button', { name: 'Show all groups' }).click()
    await expect(searchResult).toBeVisible()
    await expect(first).toHaveCount(0)
    await page.getByRole('button', { name: 'Clear search' }).click()
    await expectFirstPageOnly()
    await page.screenshot({ path: testInfo.outputPath('discovery-incomplete-list.png'), animations: 'disabled' })
  } finally {
    try {
      if (!released && pageTwoRelease != null) {
        await writeFile(pageTwoRelease, 'released\n')
      }
    } finally {
      await harness.stop()
    }
  }
}

for (const scenario of pagedDiscoveryCases) {
  if (scenario.mode === 'complete') {
    // spec: gui-shell/empty-query-discovery-receives-multiple-marketplace-pages
    test(scenario.name, async ({ page }, testInfo) => runPagedDiscoveryCase(page, testInfo, scenario))
  } else {
    // spec: gui-shell/later-marketplace-page-failure-leaves-an-honest-partial-catalog
    // spec: gui-shell/a-failed-later-page-leaves-empty-query-discovery-incomplete
    test(scenario.name, async ({ page }, testInfo) => runPagedDiscoveryCase(page, testInfo, scenario))
  }
}

// spec: gui-shell/later-marketplace-pages-are-browsable-during-and-after-search
test('built GUI keeps source-filtered browse active while Marketplace search is open', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the discovery journey')
  }
  const harness = await startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'work41-paged-discovery',
    AM_LOCALAPI_USER_DISCOVERY_MODE: 'complete',
  })
  const { bootstrap } = harness
  let browseReleased = false
  try {
    await launchBuiltUserApp(page, bootstrap)
    const firstPage = page.getByRole('option').filter({ hasText: 'paged-first' })
    const laterPage = page.getByRole('option').filter({ hasText: 'paged-second' })
    const localResource = page.getByRole('option').filter({ hasText: 'browse-local' })
    await expect(localResource).toBeVisible({ timeout: 60_000 })
    await expect(firstPage).toBeVisible({ timeout: 60_000 })
    await waitForMarker(bootstrap.discoveryPageTwoStarted)

    if (bootstrap.marketplaceEndpoint == null || bootstrap.discoveryPageTwoRelease == null) {
      throw new Error('paged Marketplace fixture markers are missing')
    }
    const browseSource = page.getByRole('button', { name: `Browse source ${bootstrap.marketplaceEndpoint}` })
    await browseSource.focus()
    await page.keyboard.press('Enter')
    const clearSource = page.getByRole('button', { name: /^Clear source filter / })
    await expect(clearSource).toBeVisible()
    await expect(localResource).toHaveCount(0)
    await expect(firstPage).toBeVisible()

    await page.getByRole('button', { name: 'Search', exact: true }).click()
    const search = page.getByRole('searchbox', { name: 'Search resources' })
    await search.fill('paged-search')
    await search.press('Enter')
    const searchResult = page.getByRole('option').filter({ hasText: 'paged-search' })
    await expect(searchResult).toBeVisible({ timeout: 60_000 })
    await expect(laterPage).toHaveCount(0)

    const diagnostics = page.getByRole('button', { name: 'Show diagnostics' })
    await diagnostics.click()
    const health = page.getByRole('region', { name: 'Marketplace discovery health' })
    await expect(health.getByText('Marketplace discovery is updating.')).toBeVisible()
    await writeFile(bootstrap.discoveryPageTwoRelease, 'released\n')
    browseReleased = true
    await expect(health.getByText('Marketplace discovery is updating.')).not.toBeVisible({ timeout: 60_000 })
    await page.screenshot({ path: testInfo.outputPath('marketplace-search-with-browse-complete.png'), animations: 'disabled' })

    await page.getByRole('button', { name: 'Show all groups' }).click()
    await expect(searchResult).toBeVisible()
    await expect(laterPage).toHaveCount(0)
    await page.getByRole('button', { name: 'Clear search' }).click()
    await expect(clearSource).toBeVisible()
    await expect(firstPage).toBeVisible()
    await expect(laterPage).toBeVisible({ timeout: 60_000 })
    await expect(localResource).toHaveCount(0)
    await page.screenshot({ path: testInfo.outputPath('marketplace-browse-restored.png'), animations: 'disabled' })
  } finally {
    try {
      if (!browseReleased && bootstrap.discoveryPageTwoRelease != null) {
        await writeFile(bootstrap.discoveryPageTwoRelease, 'released\n')
      }
    } finally {
      await harness.stop()
    }
  }
})
