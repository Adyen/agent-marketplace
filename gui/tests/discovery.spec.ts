import { expect, test, type TestInfo } from '@playwright/test'
import { existsSync } from 'node:fs'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { startHarness } from './harness'
import { distIndexExists, launchBuiltUserApp } from './app'
import { focusedDescription, tabTo } from './keyboard'

async function saveActions(testInfo: TestInfo, actions: string[]): Promise<void> {
  const path = testInfo.outputPath('discovery.actions.json')
  await mkdir(dirname(path), { recursive: true })
  await writeFile(path, `${JSON.stringify({ actions }, null, 2)}\n`)
}

async function waitForMarker(path: string | undefined): Promise<void> {
  if (path == null) {
    throw new Error('discovery harness marker missing')
  }
  await expect.poll(() => existsSync(path), { timeout: 30_000 }).toBe(true)
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
    await expect(discoveryHealth).toContainText('fresh', { timeout: 10_000 })
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
    await expect(discoveryHealth).toContainText('miss')
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
