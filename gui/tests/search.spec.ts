import { expect, test, type TestInfo } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { startHarness } from './harness'
import { distIndexExists, launchBuiltUserApp } from './app'
import { tabTo } from './keyboard'

async function saveActions(testInfo: TestInfo, actions: string[]): Promise<void> {
  const path = testInfo.outputPath('search.actions.json')
  await mkdir(dirname(path), { recursive: true })
  await writeFile(path, `${JSON.stringify({ actions }, null, 2)}\n`)
}

// spec: session-navigation/local-search-applies-immediately
// spec: session-navigation/applied-query-and-count-remain-visible
// spec: session-navigation/search-resumes-the-current-query
// spec: session-navigation/clear-and-refresh
// spec: session-navigation/status-grouped-resource-order-and-filtering
test('built GUI searches the accepted local index by keyboard and restores saved folds', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the local-search journey')
  }

  const actions: string[] = []
  const outputDir = dirname(testInfo.outputPath('search.actions.json'))
  const harnessLogDir = join(outputDir, 'harness')
  await mkdir(harnessLogDir, { recursive: true })
  const harness = await startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'navigation',
    AM_LOCALAPI_RELEASE_VERSION: 'dev',
    AM_WORK28_TELEMETRY_EVIDENCE_DIR: harnessLogDir,
  })
  const { bootstrap } = harness

  try {
    await page.setViewportSize({ width: 1280, height: 800 })
    await launchBuiltUserApp(page, bootstrap)
    const list = page.getByRole('region', { name: 'Resources' })
    const options = page.getByRole('option')
    const detail = page.getByRole('complementary', { name: 'Detail' })
    const searchButton = page.getByRole('button', { name: 'Search', exact: true })
    const pending = page.getByRole('button', { name: 'Pending sync', exact: true })
    const apiRequests: string[] = []
    page.on('request', (request) => {
      const pathname = new URL(request.url()).pathname
      if (pathname.startsWith('/api/') && pathname !== '/api/events') {
        apiRequests.push(`${request.method()} ${pathname}`)
      }
    })

    await expect(list).toBeVisible({ timeout: 60_000 })
    await expect(options).toHaveCount(60, { timeout: 60_000 })
    await page.getByRole('button', { name: 'Show diagnostics' }).click()
    await expect(page.locator('.gui-diagnostics-view')).toContainText('Drift: ready', { timeout: 60_000 })
    await page.getByRole('button', { name: 'Show all groups' }).click()
    await expect(page.getByRole('button', { name: 'Refresh', exact: true })).toBeEnabled({ timeout: 60_000 })
    actions.push('Startup ready: 60 local resources, Detail Drift: ready, Refresh enabled')

    await tabTo(page, pending, 'Pending sync section button', actions)
    await expect(pending).toHaveAttribute('aria-expanded', 'true')
    actions.push('Space: save Pending sync as folded before search')
    await page.keyboard.press('Space')
    await expect(pending).toHaveAttribute('aria-expanded', 'false')
    await expect(options).toHaveCount(0)

    await tabTo(page, searchButton, 'Search button', actions, undefined, 'Shift+Tab')
    actions.push('Enter: open Search with keyboard')
    await page.keyboard.press('Enter')
    const searchInput = page.getByRole('searchbox', { name: 'Search resources' })
    await expect(searchInput).toBeVisible()
    await expect(searchInput).toBeFocused()
    actions.push('Keyboard input is focused')
    const apiRequestsBeforeQuery = apiRequests.length
    await page.keyboard.type('skill-030')
    await expect(options).toHaveCount(1)
    const selected = page.getByRole('option', { name: /skill-030/ })
    await expect(selected).toHaveAttribute('aria-selected', 'true')
    await expect(pending).toHaveAttribute('aria-expanded', 'true')
    await expect(detail).toContainText('skill-030')
    const count = page.locator('.workspace__search-status')
    await expect(count).toContainText('1')
    expect(apiRequests).toHaveLength(apiRequestsBeforeQuery)
    actions.push('Local index filtered to one resource with no new API request and temporarily opened Pending sync')

    actions.push('Enter: confirm local query')
    await page.keyboard.press('Enter')
    await expect(searchInput).toBeHidden()
    await expect(count).toContainText('1')
    await expect(searchButton).toBeFocused()

    actions.push('Enter: resume editing the applied query')
    await page.keyboard.press('Enter')
    await expect(searchInput).toBeFocused()
    await expect(searchInput).toHaveValue('skill-030')
    await page.keyboard.press('End')
    await page.keyboard.type('x')
    await expect(options).toHaveCount(0)
    await expect(count).toContainText('0')
    actions.push('Draft query filters immediately without an API search request')

    actions.push('Escape: discard draft and keep the applied query')
    await page.keyboard.press('Escape')
    await expect(searchInput).toBeHidden()
    await expect(options).toHaveCount(1)
    await expect(count).toContainText('1')
    await expect(searchButton).toBeFocused()

    const clearButton = page.getByRole('button', { name: 'Clear search', exact: true })
    await tabTo(page, clearButton, 'Clear search button', actions)
    actions.push('Enter: clear local search')
    await page.keyboard.press('Enter')
    await expect(count).toHaveCount(0)
    await expect(pending).toHaveAttribute('aria-expanded', 'false')
    await expect(options).toHaveCount(0)

    await tabTo(page, pending, 'Pending sync section button after clearing search', actions)
    actions.push('Space: expand the restored saved section')
    await page.keyboard.press('Space')
    await expect(pending).toHaveAttribute('aria-expanded', 'true')
    await expect(options).toHaveCount(60)
    await expect(options.first()).toContainText('skill-000')
    await expect(options.last()).toContainText('skill-059')
    actions.push('Clear restored alphabetical rows and the ordinary saved fold state')
    await page.screenshot({ path: testInfo.outputPath('local-search-cleared.png'), fullPage: true, animations: 'disabled' })
  } finally {
    await harness.stop()
    await saveActions(testInfo, actions)
  }
})
