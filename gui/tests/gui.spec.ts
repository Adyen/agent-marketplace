import { test, expect } from '@playwright/test'
import { existsSync, readdirSync } from 'node:fs'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { startHarness, repoRoot } from './harness'

const dist = join(repoRoot, 'cmd', 'agent-marketplace-gui', 'frontend', 'dist')

const contentTypes: Record<string, string> = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.woff2': 'font/woff2',
}

function ext(path: string): string {
  const index = path.lastIndexOf('.')
  return index === -1 ? '' : path.slice(index)
}

async function serveBuiltApp(page: import('@playwright/test').Page, url: string) {
  await page.route(`${url}/**`, async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path.startsWith('/api/')) {
      await route.continue()
      return
    }
    const relative = path === '/' ? 'index.html' : path.replace(/^\/+/, '')
    const file = join(dist, relative)
    if (!existsSync(file)) {
      await route.fulfill({ status: 404, body: 'not found' })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: contentTypes[ext(file)] ?? 'application/octet-stream',
      body: await readFile(file),
    })
  })
}

test('built GUI drives the real user-scope core', async ({ page }) => {
  if (!existsSync(join(dist, 'index.html'))) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }

  const harness = await startHarness('user')
  const { bootstrap } = harness
  const home = bootstrap.home!
  const userConfigPath = join(home, '.agents', 'config.yaml')
  const configBefore = await readFile(userConfigPath)
  const materialized = join(home, '.config', 'devin', 'skills', 'solo', 'SKILL.md')

  try {
    await serveBuiltApp(page, bootstrap.url)
    await page.addInitScript(
      ([credential]) => {
        ;(window as unknown as Record<string, unknown>).__AM_GUI_BOOTSTRAP__ = { credential }
      },
      [bootstrap.credential] as const,
    )
    await page.goto(bootstrap.url, { waitUntil: 'domcontentloaded' })
    await expect(page.getByText('skill/solo')).toBeVisible({ timeout: 60_000 })
    await expect(page.getByRole('button', { name: 'Refresh' })).toBeEnabled()
    await expect(page.getByRole('button', { name: 'Sync' })).toBeEnabled()

    await page.getByRole('button', { name: 'Refresh' }).focus()
    await page.keyboard.press('Enter')
    await expect(page.getByText('skill/solo')).toBeVisible()

    await expect(page.getByRole('button', { name: 'Sync' })).toBeEnabled()
    await page.getByRole('button', { name: 'Sync' }).focus()
    await page.keyboard.press('Enter')
    await expect(page.getByText('sync complete')).toBeVisible({ timeout: 60_000 })
    const body = await readFile(materialized, 'utf8')
    expect(body).toContain('Solo')

    const configAfter = await readFile(userConfigPath)
    expect(configAfter.equals(configBefore)).toBe(true)

    await expect(page.getByRole('button', { name: 'Update' })).toBeEnabled()
    await page.getByRole('button', { name: 'Update' }).focus()
    await page.keyboard.press('Enter')
    await expect(page.getByText('update complete')).toBeVisible({ timeout: 60_000 })
  } finally {
    await harness.stop()
  }
})

// spec: gui-configuration/save-user-harness-targets-without-losing-unknown-identifiers
// spec: gui-configuration/organization-harness-targets-are-read-only
// spec: gui-configuration/harness-target-edit-matches-the-cli-mutation
// spec: session-interactions/toggling-harness-targets-does-not-write
// spec: session-interactions/save-selected-harnesses
// spec: session-interactions/empty-selection
test('built GUI saves Harness targets through the real user-scope core', async ({ page }) => {
  if (!existsSync(join(dist, 'index.html'))) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }

  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'harnesses' })
  const { bootstrap } = harness
  const home = bootstrap.home!
  const userConfigPath = join(home, '.agents', 'config.yaml')
  const fixtureConfig = (targets: string) =>
    `apiVersion: agent-marketplace/v4\nharnesses: ${targets}\nsources:\n  local: {directory: ${join(home, 'fixture-source')}}\nresources:\n  local: {all: true}\n`

  try {
    await serveBuiltApp(page, bootstrap.url)
    await page.addInitScript(
      ([credential]) => {
        ;(window as unknown as Record<string, unknown>).__AM_GUI_BOOTSTRAP__ = { credential }
      },
      [bootstrap.credential] as const,
    )
    await page.goto(bootstrap.url, { waitUntil: 'domcontentloaded' })
    await expect(page.getByText('skill/solo')).toBeVisible({ timeout: 60_000 })

    const before = await readFile(userConfigPath, 'utf8')
    expect(before).toBe(fixtureConfig('[devin, e2e-future-harness]'))

    const settingsButton = page.getByRole('button', { name: 'Settings' })
    await settingsButton.focus()
    await page.keyboard.press('Enter')
    const dialog = page.getByRole('dialog', { name: 'Harness targets' })
    await expect(dialog).toBeVisible()

    const devin = page.getByRole('checkbox', { name: 'Target devin' })
    const future = page.getByRole('checkbox', { name: 'Target e2e-future-harness' })
    await expect(future).toBeChecked()
    await expect(dialog.getByText('Unsupported by this version')).toBeVisible()
    await expect(dialog.getByLabel('Organization targets')).toContainText('claude')
    await expect(dialog.getByLabel('Organization targets').getByRole('checkbox')).toHaveCount(0)
    await expect(dialog).toContainText('managed outside the GUI')

    await devin.focus()
    await page.keyboard.press('Space')
    await expect(devin).not.toBeChecked()
    await page.keyboard.press('Escape')
    await expect(dialog).not.toBeVisible()
    await expect(settingsButton).toBeFocused()
    expect(await readFile(userConfigPath, 'utf8')).toBe(before)

    await settingsButton.press('Enter')
    await expect(dialog).toBeVisible()
    await expect(devin).toBeChecked()
    await devin.press('Space')
    await expect(devin).not.toBeChecked()
    await expect(future).toBeChecked()

    await page.getByRole('button', { name: 'Save targets' }).press('Enter')
    await expect(page.getByText('targets complete')).toBeVisible({ timeout: 60_000 })
    await expect(dialog).not.toBeVisible()
    await expect(settingsButton).toBeFocused()

    const saved = await readFile(userConfigPath, 'utf8')
    expect(saved).toBe(fixtureConfig('[e2e-future-harness]'))

    await expect(page.getByRole('button', { name: 'Sync' })).toBeDisabled()
    expect(await readFile(userConfigPath, 'utf8')).toBe(saved)
    expect(existsSync(join(home, '.e2e-future-harness'))).toBe(false)
  } finally {
    await harness.stop()
  }
})

// spec: gui-first-run/absent-user-personal-root-starts-first-run
// spec: gui-first-run/detected-harness-targets-are-preselected-and-editable
// spec: gui-first-run/unmanaged-first-run-presents-init-equivalent-consent
// spec: gui-first-run/cancel-first-run-before-completion
// spec: gui-first-run/harness-targets-and-consent-match-init-output
test('built GUI guided first-run setup initializes the absent root by keyboard', async ({ page }) => {
  if (!existsSync(join(dist, 'index.html'))) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }

  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'first-run' })
  const { bootstrap } = harness
  const home = bootstrap.home!
  const repo = bootstrap.repo!
  const userConfigPath = join(home, '.agents', 'config.yaml')
  const repoConfigPath = join(repo, '.agents', 'config.yaml')
  const repoLocalPath = join(repo, '.agents', 'config.local.yaml')
  const repoConfigBefore = await readFile(repoConfigPath)
  const repoLocalBefore = await readFile(repoLocalPath)
  const xdgDirs = [bootstrap.xdgConfig!, bootstrap.xdgState!, bootstrap.xdgCache!]

  const xdgEntries = () =>
    xdgDirs.map((dir) => (existsSync(dir) ? readdirSync(dir).sort().join(',') : '<absent>'))

  try {
    expect(existsSync(userConfigPath)).toBe(false)

    await serveBuiltApp(page, bootstrap.url)
    await page.addInitScript(
      ([credential]) => {
        ;(window as unknown as Record<string, unknown>).__AM_GUI_BOOTSTRAP__ = { credential }
      },
      [bootstrap.credential] as const,
    )
    await page.goto(bootstrap.url, { waitUntil: 'domcontentloaded' })

    const dialog = page.getByRole('dialog', { name: 'Set up Agent Marketplace' })
    await expect(dialog).toBeVisible({ timeout: 60_000 })

    const factory = dialog.getByRole('checkbox', { name: 'Target factory' })
    const devin = dialog.getByRole('checkbox', { name: 'Target devin' })
    await expect(factory).toBeChecked()
    await expect(devin).not.toBeChecked()

    await factory.focus()
    await page.keyboard.press('Space')
    await expect(factory).not.toBeChecked()
    await expect(dialog.getByRole('button', { name: 'Next' })).toBeDisabled()
    await page.keyboard.press('Space')
    await expect(factory).toBeChecked()
    await devin.focus()
    await page.keyboard.press('Space')
    await expect(devin).toBeChecked()

    await page.keyboard.press('Escape')
    await expect(dialog).not.toBeVisible()
    expect(existsSync(userConfigPath)).toBe(false)
    await page.waitForTimeout(300)
    await expect(dialog).not.toBeVisible()
    expect(existsSync(userConfigPath)).toBe(false)
    expect((await readFile(repoConfigPath)).equals(repoConfigBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    expect(xdgEntries()).toEqual(['<absent>', '<absent>', '<absent>'])

    const setupButton = page.getByRole('button', { name: 'Set up' })
    await expect(setupButton).toBeVisible()
    await setupButton.focus()
    await page.keyboard.press('Enter')
    await expect(dialog).toBeVisible()
    await expect(factory).toBeChecked()
    await expect(devin).not.toBeChecked()

    await dialog.getByRole('button', { name: 'Next' }).focus()
    await page.keyboard.press('Enter')

    const finish = dialog.getByRole('button', { name: 'Finish setup' })
    await expect(finish).toBeDisabled()
    await expect(dialog).toContainText('basic telemetry')
    const no = dialog.getByRole('radio', { name: /no/i })
    const yes = dialog.getByRole('radio', { name: /yes/i })
    await expect(no).not.toBeChecked()
    await expect(yes).not.toBeChecked()

    await dialog.getByRole('button', { name: 'Back' }).press('Enter')
    await expect(factory).toBeChecked()
    await dialog.getByRole('button', { name: 'Next' }).press('Enter')
    await yes.focus()
    await page.keyboard.press('Space')
    await expect(yes).toBeChecked()
    await expect(finish).toBeEnabled()
    expect(existsSync(userConfigPath)).toBe(false)

    await finish.press('Enter')
    await expect(page.getByText('mutation complete')).toBeVisible({ timeout: 60_000 })
    await expect(dialog).not.toBeVisible()

    const written = await readFile(userConfigPath, 'utf8')
    expect(written).toBe(
      'apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\ntelemetry:\n  level: basic\n',
    )
    expect(existsSync(join(home, '.factory', 'skills'))).toBe(false)
    expect(existsSync(join(home, '.devin'))).toBe(false)
    expect((await readFile(repoConfigPath)).equals(repoConfigBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    expect(xdgEntries()).toEqual(['<absent>', '<absent>', '<absent>'])
  } finally {
    await harness.stop()
  }
})
