import { expect, test, type Locator, type TestInfo } from '@playwright/test'
import { existsSync } from 'node:fs'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { startHarness } from './harness'
import { distIndexExists, launchBuiltUserApp } from './app'
import { tabTo } from './keyboard'

async function relativeTop(locator: Locator): Promise<number> {
  return locator.evaluate((element) => {
    const root = element.closest('.workspace__list')
    if (!(root instanceof HTMLElement)) {
      throw new Error('selected row has no resource list scroll root')
    }
    return element.getBoundingClientRect().top - root.getBoundingClientRect().top
  })
}

async function saveActions(testInfo: TestInfo, actions: string[]): Promise<void> {
  const path = testInfo.outputPath('navigation.actions.json')
  await mkdir(dirname(path), { recursive: true })
  await writeFile(path, `${JSON.stringify({ actions }, null, 2)}\n`)
}

async function expectOptionalFileUnchanged(path: string, before: Buffer | null): Promise<void> {
  if (before == null) {
    expect(existsSync(path)).toBe(false)
    return
  }
  expect((await readFile(path)).equals(before)).toBe(true)
}

// spec: session-navigation/status-grouped-resource-order-and-filtering
// spec: session-startup/selection-follows-its-resource-when-rows-are-inserted
// spec: session-startup/an-open-detail-follows-its-verified-row
test('built GUI preserves a selected resource through a real directory refresh and keyboard folds', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the navigation journey')
  }

  const actions: string[] = []
  const outputDir = dirname(testInfo.outputPath('navigation.actions.json'))
  const harnessLogDir = join(outputDir, 'harness')
  await mkdir(harnessLogDir, { recursive: true })
  const harness = await startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'navigation',
    AM_LOCALAPI_RELEASE_VERSION: 'dev',
    AM_WORK28_TELEMETRY_EVIDENCE_DIR: harnessLogDir,
  })
  const { bootstrap } = harness
  const home = bootstrap.home!
  const repo = bootstrap.repo!
  const sourceDir = bootstrap.navigationSourceDir!
  const userConfigPath = join(home, '.agents', 'config.yaml')
  const personalLocalPath = join(home, '.agents', 'config.local.yaml')
  const repoConfigPath = join(repo, '.agents', 'config.yaml')
  const repoLocalPath = join(repo, '.agents', 'config.local.yaml')

  try {
    expect(bootstrap.orgConfig).toBeUndefined()
    const userBefore = await readFile(userConfigPath)
    const repoBefore = await readFile(repoConfigPath)
    const repoLocalBefore = await readFile(repoLocalPath)
    const personalLocalBefore = existsSync(personalLocalPath) ? await readFile(personalLocalPath) : null

    await page.setViewportSize({ width: 1280, height: 800 })
    await launchBuiltUserApp(page, bootstrap)
    const listRoot = page.locator('.workspace__list')
    const detail = page.getByRole('complementary', { name: 'Detail' })
    const refresh = page.getByRole('button', { name: 'Refresh', exact: true })
    await expect(listRoot).toBeVisible({ timeout: 60_000 })
    await expect(page.getByRole('option')).toHaveCount(60, { timeout: 60_000 })
    await expect(detail).toContainText('Drift: ready', { timeout: 60_000 })
    await expect(refresh).toBeEnabled({ timeout: 60_000 })
    await expect.poll(() => listRoot.evaluate((element) => element.scrollHeight > element.clientHeight)).toBe(true)
    actions.push('Startup ready: 60 resources, Detail Drift: ready, Refresh enabled')

    const selected = page.getByRole('option', { name: /skill-030/ })
    await expect(selected).toBeVisible()
    await tabTo(page, selected, 'mid-list resource skill-030', actions)
    actions.push('Enter: select skill-030')
    await page.keyboard.press('Enter')
    await expect(selected).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('complementary', { name: 'Detail' })).toContainText('skill-030')
    const selectedDOMID = await selected.getAttribute('id')
    if (selectedDOMID == null || !selectedDOMID.startsWith('row-')) {
      throw new Error('selected resource has no stable row DOM identity')
    }
    const selectedID = selectedDOMID.slice('row-'.length)
    const scrollTopBefore = await listRoot.evaluate((element) => element.scrollTop)
    await tabTo(page, refresh, 'Refresh button', actions, undefined, 'Shift+Tab')
    await expect(refresh).toBeEnabled()
    await listRoot.evaluate((element, scrollTop) => {
      element.scrollTop = scrollTop
    }, scrollTopBefore)
    const viewportBefore = await selected.evaluate((element) => {
      const root = element.closest('.workspace__list')
      if (!(root instanceof HTMLElement)) {
        throw new Error('selected row has no resource list scroll root')
      }
      const elementRect = element.getBoundingClientRect()
      const rootRect = root.getBoundingClientRect()
      return { top: elementRect.top - rootRect.top, bottom: elementRect.bottom - rootRect.top, height: rootRect.height }
    })
    expect(viewportBefore.top).toBeGreaterThanOrEqual(0)
    expect(viewportBefore.bottom).toBeLessThanOrEqual(viewportBefore.height)
    const topBeforeRefresh = await relativeTop(selected)
    const scrollTopBeforeRefresh = await listRoot.evaluate((element) => element.scrollTop)
    actions.push(`Refresh setup retained skill-030 at ${topBeforeRefresh.toFixed(1)}px with scrollTop ${scrollTopBeforeRefresh}`)

    const earlierName = 'skill-000-new'
    await mkdir(join(sourceDir, earlierName), { recursive: true })
    await writeFile(
      join(sourceDir, earlierName, 'SKILL.md'),
      `---\nname: ${earlierName}\ndescription: Added before the selected navigation row\n---\n# ${earlierName}\n`,
    )
    await expect(page.getByRole('option')).toHaveCount(60)
    actions.push('After writing the private fixture file, the visible list remains at 60 until Refresh')
    const refreshResponse = page.waitForResponse((response) =>
      response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/snapshot',
    )
    actions.push('Enter: request real directory refresh')
    await page.keyboard.press('Enter')
    const response = await refreshResponse
    expect(response.status()).toBe(200)
    const finalSnapshot = await response.json() as { stage: string; drift: string; rows: Array<{ id: string }> }
    expect(finalSnapshot.stage).toBe('final')
    expect(finalSnapshot.drift).toBe('ready')
    actions.push(`Refresh response: stage=${finalSnapshot.stage}, drift=${finalSnapshot.drift}, rows=${finalSnapshot.rows.length}`)
    expect(finalSnapshot.rows.some((row) => row.id === selectedID)).toBe(true)

    const refreshedSelected = page.getByRole('option', { name: /skill-030/ })
    await expect(page.getByRole('option', { name: /skill-000-new/ })).toBeVisible({ timeout: 60_000 })
    await expect(page.getByRole('option')).toHaveCount(61, { timeout: 60_000 })
    await expect(refreshedSelected).toHaveAttribute('id', selectedDOMID)
    await expect(refreshedSelected).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('complementary', { name: 'Detail' })).toContainText('skill-030')
    const topAfter = await relativeTop(refreshedSelected)
    const scrollTopAfter = await listRoot.evaluate((element) => element.scrollTop)
    actions.push(`Refreshed skill-030 viewport anchor at ${topAfter.toFixed(1)}px with scrollTop ${scrollTopAfter}`)
    expect(Math.abs(topAfter - topBeforeRefresh)).toBeLessThanOrEqual(1)

    const pending = page.getByRole('button', { name: 'Pending sync', exact: true })
    await expect(pending).toBeVisible()
    actions.push('Pending sync group is present before materialization')
    await tabTo(page, pending, 'Pending sync status section button', actions)
    await expect(pending).toHaveAttribute('aria-expanded', 'true')
    actions.push('Space: collapse Pending sync status section')
    await page.keyboard.press('Space')
    await expect(pending).toHaveAttribute('aria-expanded', 'false')
    await expect(refreshedSelected).toHaveCount(0)
    await expect(page.getByRole('option')).toHaveCount(0)
    await expect(pending).toHaveAttribute('aria-current', 'true')
    actions.push('Space: expand Pending sync status section')
    await page.keyboard.press('Space')
    await expect(pending).toHaveAttribute('aria-expanded', 'true')
    await expect(page.getByRole('option')).toHaveCount(61)

    expect((await readFile(userConfigPath)).equals(userBefore)).toBe(true)
    expect((await readFile(repoConfigPath)).equals(repoBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    await expectOptionalFileUnchanged(personalLocalPath, personalLocalBefore)
    await page.screenshot({ path: testInfo.outputPath('navigation-folds-and-anchor.png'), fullPage: true, animations: 'disabled' })
    actions.push('Refresh preserved the selected resource identity, detail, and viewport; fold controls worked by keyboard')
  } finally {
    await harness.stop()
    await saveActions(testInfo, actions)
  }
})
