import { test, expect } from '@playwright/test'
import { existsSync } from 'node:fs'
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
