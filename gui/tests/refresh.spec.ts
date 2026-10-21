import { test, expect, type Page, type TestInfo } from '@playwright/test'
import { readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { startHarness, type Bootstrap } from './harness'
import { distIndexExists, serveBuiltApp } from './app'

async function launchBuiltUserApp(page: Page, bootstrap: Bootstrap) {
  await serveBuiltApp(page, bootstrap.url)
  await page.addInitScript(
    ([credential]) => {
      ;(window as unknown as Record<string, unknown>).__AM_GUI_BOOTSTRAP__ = { credential }
    },
    [bootstrap.credential] as const,
  )
  await page.goto(bootstrap.url, { waitUntil: 'domcontentloaded' })
}

function artifactPath(testInfo: TestInfo, name: string): string {
  const directory = process.env.AM_WORK25_REPAIR_EVIDENCE_DIR
  return directory == null ? testInfo.outputPath(name) : join(directory, name)
}

function countSnapshotRequests(page: Page): () => number {
  let count = 0
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/snapshot') {
      count += 1
    }
  })
  return () => count
}

// spec: gui-shell/refresh-and-focus-reload-use-ordinary-snapshot-behavior
// spec: gui-shell/external-file-changes-require-a-refresh-trigger
// spec: gui-shell/launch-directory-does-not-change-the-gui-workspace
test('built GUI loads external user-config changes only on refresh triggers', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }

  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'harnesses' })
  const { bootstrap } = harness
  const home = bootstrap.home!
  const repo = bootstrap.repo!
  const orgConfig = bootstrap.orgConfig!
  const userConfigPath = join(home, '.agents', 'config.yaml')
  const source = join(home, 'fixture-source')
  const userConfig = (targets: string) =>
    `apiVersion: agent-marketplace/v4\nharnesses: ${targets}\nsources:\n  local: {directory: ${source}}\nresources:\n  local: {all: true}\n`
  const repoConfigPath = join(repo, '.agents', 'config.yaml')
  const repoLocalPath = join(repo, '.agents', 'config.local.yaml')
  const repoConfigBefore = await readFile(repoConfigPath)
  const repoLocalBefore = await readFile(repoLocalPath)
  const orgBefore = await readFile(orgConfig)

  try {
    const snapshotRequests = countSnapshotRequests(page)
    await launchBuiltUserApp(page, bootstrap)
    await expect(page.getByRole('option').filter({ hasText: 'solo' }).first()).toBeVisible({ timeout: 60_000 })

    const settingsButton = page.getByRole('button', { name: 'Settings' })
    const dialog = page.getByRole('dialog', { name: 'Settings' })
    const saveTargets = dialog.getByRole('button', { name: 'Save targets' })
    const detail = page.locator('.workspace__detail')
    const sync = page.getByRole('button', { name: 'Sync', exact: true })
    const devin = dialog.getByRole('checkbox', { name: 'Target devin' })
    const claude = dialog.getByRole('checkbox', { name: 'Target claude' })

    await expect(detail).toContainText('Drift: ready')
    await expect(sync).toBeEnabled()
    await expect(detail).not.toContainText('claude')

    await writeFile(userConfigPath, userConfig('[devin, claude]'))
    await page.waitForTimeout(1000)
    expect(snapshotRequests()).toBe(0)
    await expect(page.getByRole('option').filter({ hasText: 'solo' }).first()).toBeVisible()
    await expect(detail).toContainText('Drift: ready')
    await expect(detail).not.toContainText('claude')
    await page.screenshot({ path: artifactPath(testInfo, 'refresh-workspace-before-trigger.png'), fullPage: true })

    const refresh = page.getByRole('button', { name: 'Refresh' })
    await refresh.focus()
    await page.keyboard.press('Enter')
    await expect.poll(() => snapshotRequests()).toBe(1)
    await expect(detail).toContainText('claude', { timeout: 60_000 })
    await expect(detail).toContainText('Drift: ready')
    await expect(sync).toBeEnabled()
    await settingsButton.press('Enter')
    await expect(dialog).toBeVisible()
    await expect(saveTargets).toBeEnabled()
    await expect(devin).toBeChecked()
    await expect(claude).toBeChecked()
    await page.screenshot({ path: artifactPath(testInfo, 'refresh-settings-after-explicit.png'), fullPage: true })
    await page.keyboard.press('Escape')
    await expect(dialog).not.toBeVisible()

    await writeFile(userConfigPath, userConfig('[devin]'))
    await page.waitForTimeout(1000)
    expect(snapshotRequests()).toBe(1)
    await expect(detail).toContainText('claude')
    await page.evaluate(() => window.dispatchEvent(new Event('focus')))
    await expect.poll(() => snapshotRequests()).toBe(2)
    await expect(detail).not.toContainText('claude', { timeout: 60_000 })
    await expect(detail).toContainText('Drift: ready')
    await expect(sync).toBeEnabled()
    await settingsButton.press('Enter')
    await expect(dialog).toBeVisible()
    await expect(saveTargets).toBeEnabled()
    await expect(devin).toBeChecked()
    await expect(claude).not.toBeChecked()
    await page.screenshot({ path: artifactPath(testInfo, 'refresh-settings-after-focus.png'), fullPage: true })
    await page.keyboard.press('Escape')

    expect((await readFile(repoConfigPath)).equals(repoConfigBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    expect((await readFile(orgConfig)).equals(orgBefore)).toBe(true)
    expect(await readFile(userConfigPath, 'utf8')).toBe(userConfig('[devin]'))
  } finally {
    await harness.stop()
  }
})
