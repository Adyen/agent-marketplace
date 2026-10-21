import { test, expect, type Page, type TestInfo } from '@playwright/test'
import { existsSync } from 'node:fs'
import { readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { startHarness, type Bootstrap } from './harness'
import { distIndexExists, serveBuiltApp } from './app'

function artifactPath(testInfo: TestInfo, name: string): string {
  const directory = process.env.AM_WORK26_REPAIR_EVIDENCE_DIR
  return directory == null ? testInfo.outputPath(name) : join(directory, name)
}

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

async function launchWithNativeBridge(page: Page, bootstrap: Bootstrap) {
  await serveBuiltApp(page, bootstrap.url)
  await page.addInitScript(
    ([credential]) => {
      const w = window as unknown as Record<string, unknown> & {
        __quitCount: number
        __closeRequest: () => void
        runtime?: { EventsOn: (event: string, callback: () => void) => () => void }
        go?: { main: { App: { Quit: () => Promise<void> } } }
      }
      const listeners: Record<string, (() => void) | undefined> = {}
      w.__quitCount = 0
      w.__closeRequest = () => listeners['desktop:close-request']?.()
      w.runtime = {
        EventsOn: (event: string, callback: () => void) => {
          listeners[event] = callback
          return () => {
            delete listeners[event]
          }
        },
      }
      w.go = {
        main: {
          App: {
            Quit: () => {
              w.__quitCount += 1
              return Promise.resolve()
            },
          },
        },
      }
      w.__AM_GUI_BOOTSTRAP__ = { credential }
    },
    [bootstrap.credential] as const,
  )
  await page.goto(bootstrap.url, { waitUntil: 'domcontentloaded' })
}

function quitCount(page: Page): Promise<number> {
  return page.evaluate(() => (window as unknown as { __quitCount: number }).__quitCount)
}

function closeRequest(page: Page): Promise<void> {
  return page.evaluate(() => (window as unknown as { __closeRequest: () => void }).__closeRequest())
}

function observeRequests(page: Page, pathname: string): string[] {
  const bodies: string[] = []
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === pathname) {
      bodies.push(request.postData() ?? '')
    }
  })
  return bodies
}

// spec: session-interactions/confirmation-retains-all-user-choices
test('built GUI drives ordinary quit choices through the native Quit bridge', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }

  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'pending-sync' })
  const { bootstrap } = harness

  try {
    const actionBodies = observeRequests(page, '/api/actions')
    await launchWithNativeBridge(page, bootstrap)

    await expect(page.getByRole('option').filter({ hasText: 'solo' }).first()).toBeVisible({ timeout: 60_000 })

    await page.getByRole('button', { name: 'Sync', exact: true }).press('Enter')
    await expect(page.getByText('sync complete')).toBeVisible({ timeout: 60_000 })
    await expect.poll(() => actionBodies.filter((b) => JSON.parse(b).action === 'sync').length).toBe(1)

    const quit = page.getByRole('button', { name: 'Quit', exact: true })
    const quitDialog = page.getByRole('dialog', { name: 'Quit Agent Marketplace' })

    await quit.press('Enter')
    await expect(quitDialog).toBeVisible()
    await expect(quitDialog.getByRole('button', { name: 'Quit now' })).toBeVisible()
    await quitDialog.screenshot({ path: artifactPath(testInfo, 'quit-dialog-settled.png'), animations: 'disabled' })
    await quitDialog.getByRole('button', { name: 'Stay' }).press('Enter')
    await expect(quitDialog).not.toBeVisible()
    expect(await quitCount(page)).toBe(0)

    await closeRequest(page)
    await expect(quitDialog).toBeVisible()
    await quitDialog.getByRole('button', { name: 'Cancel quit' }).press('Enter')
    await expect(quitDialog).not.toBeVisible()
    expect(await quitCount(page)).toBe(0)

    await closeRequest(page)
    await expect(quitDialog).toBeVisible()
    await quitDialog.getByRole('button', { name: 'Quit now' }).press('Enter')
    await expect.poll(() => quitCount(page)).toBe(1)
    expect(actionBodies.filter((b) => JSON.parse(b).action === 'sync')).toHaveLength(1)
  } finally {
    await harness.stop()
  }
})

// spec: session-interactions/confirmation-retains-all-user-choices
test('built GUI pending-change quit offers sync-and-quit and quit-without-syncing', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }

  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'pending-sync' })
  const { bootstrap } = harness
  const home = bootstrap.home!
  const materialized = join(home, '.config', 'devin', 'skills', 'solo', 'SKILL.md')
  const sourceSkill = await readFile(join(home, 'fixture-source', 'solo', 'SKILL.md'))

  try {
    const actionBodies = observeRequests(page, '/api/actions')
    await launchWithNativeBridge(page, bootstrap)


    const quit = page.getByRole('button', { name: 'Quit', exact: true })
    const quitDialog = page.getByRole('dialog', { name: 'Quit Agent Marketplace' })

    await quit.press('Enter')
    await expect(quitDialog).toBeVisible()
    await expect(quitDialog.getByRole('button', { name: 'Sync and quit' })).toBeEnabled()
    await quitDialog.screenshot({ path: artifactPath(testInfo, 'quit-pending-changes-dialog.png'), animations: 'disabled' })
    await quitDialog.getByRole('button', { name: 'Cancel quit' }).press('Enter')
    await expect(quitDialog).not.toBeVisible()
    expect(await quitCount(page)).toBe(0)
    expect(actionBodies).toHaveLength(0)

    await quit.press('Enter')
    await expect(quitDialog).toBeVisible()
    await quitDialog.getByRole('button', { name: 'Quit without syncing' }).press('Enter')
    await expect.poll(() => quitCount(page)).toBe(1)
    await expect(quitDialog).not.toBeVisible()
    expect(actionBodies).toHaveLength(0)

    await page.evaluate(() => {
      ;(window as unknown as { __quitCount: number }).__quitCount = 0
    })
    await quit.press('Enter')
    await expect(quitDialog).toBeVisible()
    await quitDialog.getByRole('button', { name: 'Sync and quit' }).press('Enter')
    await expect.poll(() => actionBodies.length).toBe(1)
    expect(JSON.parse(actionBodies[0])).toEqual({ action: 'sync' })
    await expect(page.getByText('sync complete')).toBeVisible({ timeout: 60_000 })
    await expect.poll(() => quitCount(page)).toBe(1)
    expect(actionBodies).toHaveLength(1)
    expect((await readFile(materialized)).equals(sourceSkill)).toBe(true)
  } finally {
    await harness.stop()
  }
})

// spec: session-interactions/confirmation-retains-all-user-choices
// spec: session-interactions/cancel-or-stale-completion
test('built GUI attaches native close to a running sync and quits on its settlement', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }

  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'held-sync' })
  const { bootstrap } = harness
  const home = bootstrap.home!
  const syncStarted = bootstrap.syncStarted!
  const syncRelease = bootstrap.syncRelease!
  const materialized = join(home, '.config', 'devin', 'skills', 'solo', 'SKILL.md')
  const sourceSkill = '---\nname: solo\ndescription: Solo skill\n---\n# Solo\n'

  try {
    const actionBodies = observeRequests(page, '/api/actions')
    await launchWithNativeBridge(page, bootstrap)
    await expect(page.getByRole('option').filter({ hasText: 'solo' }).first()).toBeVisible({ timeout: 60_000 })

    await page.getByRole('button', { name: 'Sync', exact: true }).press('Enter')
    await expect.poll(() => actionBodies.filter((b) => JSON.parse(b).action === 'sync').length).toBe(1)
    await expect.poll(() => existsSync(syncStarted)).toBe(true)

    const quitDialog = page.getByRole('dialog', { name: 'Quit Agent Marketplace' })
    await closeRequest(page)
    await expect(quitDialog).toBeVisible()
    await expect(quitDialog).toContainText('sync is running')
    await quitDialog.screenshot({ path: artifactPath(testInfo, 'native-close-during-sync.png'), animations: 'disabled' })
    await quitDialog.getByRole('button', { name: 'Cancel quit' }).press('Enter')
    await expect(quitDialog).not.toBeVisible()
    expect(await quitCount(page)).toBe(0)

    await closeRequest(page)
    await expect(quitDialog).toBeVisible()
    await quitDialog.getByRole('button', { name: 'Sync and quit' }).press('Enter')
    await expect.poll(() => actionBodies.filter((b) => JSON.parse(b).action === 'sync').length).toBe(1)
    expect(await quitCount(page)).toBe(0)

    await writeFile(syncRelease, '1')
    await expect(page.getByText('sync complete')).toBeVisible({ timeout: 60_000 })
    await expect.poll(() => quitCount(page)).toBe(1)
    expect(await readFile(materialized, 'utf8')).toBe(sourceSkill)
    await page.screenshot({ path: artifactPath(testInfo, 'native-close-sync-settled.png'), fullPage: true, animations: 'disabled' })
  } finally {
    await harness.stop()
  }
})

// spec: gui-shell/configuration-actions-do-not-open-a-raw-editor
test('built GUI keyboard input never opens a raw configuration editor', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }

  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'harnesses' })
  const { bootstrap } = harness
  const userConfigPath = join(bootstrap.home!, '.agents', 'config.yaml')
  const repoConfigPath = join(bootstrap.repo!, '.agents', 'config.yaml')
  const repoLocalPath = join(bootstrap.repo!, '.agents', 'config.local.yaml')
  const orgConfig = bootstrap.orgConfig!
  const userBefore = await readFile(userConfigPath)
  const repoConfigBefore = await readFile(repoConfigPath)
  const repoLocalBefore = await readFile(repoLocalPath)
  const orgBefore = await readFile(orgConfig)

  try {
    const actionBodies = observeRequests(page, '/api/actions')
    const configureBodies = observeRequests(page, '/api/configuration/actions')
    await launchBuiltUserApp(page, bootstrap)
    await expect(page.getByRole('option').filter({ hasText: 'solo' }).first()).toBeVisible({ timeout: 60_000 })

    for (const name of ['Sync', 'Refresh', 'Settings', 'Sources', 'Quit']) {
      const button = page.getByRole('button', { name, exact: true })
      if (await button.isVisible().catch(() => false)) {
        await button.press('e')
        await button.press('E')
      }
    }

    const settingsDialog = page.getByRole('dialog', { name: 'Settings' })
    await page.getByRole('button', { name: 'Settings', exact: true }).press('Enter')
    await expect(settingsDialog).toBeVisible()
    await settingsDialog.screenshot({ path: artifactPath(testInfo, 'no-editor-settings-open.png'), animations: 'disabled' })
    for (const control of await settingsDialog.getByRole('button').all()) {
      if (await control.isVisible().catch(() => false)) {
        await control.press('e')
        await control.press('E')
      }
    }
    await settingsDialog.getByRole('button', { name: 'Cancel' }).press('Enter')
    await expect(settingsDialog).not.toBeVisible()

    const sourcesDialog = page.getByRole('dialog', { name: 'Sources' })
    await page.getByRole('button', { name: 'Sources', exact: true }).press('Enter')
    await expect(sourcesDialog).toBeVisible()
    for (const control of await sourcesDialog.getByRole('button').all()) {
      if (await control.isVisible().catch(() => false)) {
        await control.press('e')
      }
    }
    await sourcesDialog.getByRole('button', { name: 'Done' }).press('Enter')
    await expect(sourcesDialog).not.toBeVisible()

    await expect(page.locator('textarea')).toHaveCount(0)
    await expect(page.locator('[contenteditable="true"]')).toHaveCount(0)
    expect(actionBodies).toHaveLength(0)
    expect(configureBodies).toHaveLength(0)
    expect((await readFile(userConfigPath)).equals(userBefore)).toBe(true)
    expect((await readFile(repoConfigPath)).equals(repoConfigBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    expect((await readFile(orgConfig)).equals(orgBefore)).toBe(true)
  } finally {
    await harness.stop()
  }
})
