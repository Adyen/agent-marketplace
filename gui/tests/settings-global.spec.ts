import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { distIndexExists, launchBuiltUserApp } from './app'
import { startHarness } from './harness'

// spec: gui-configuration/settings-save-writes-the-user-yaml
test('built GUI saves a global setting through the real user-scope core', async ({ page }) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }

  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'sources' })
  const { bootstrap } = harness
  const userConfigPath = join(bootstrap.home!, '.agents', 'config.yaml')
  try {
    await launchBuiltUserApp(page, bootstrap)
    await expect(page.getByText('solo', { exact: true }).first()).toBeVisible({ timeout: 60_000 })

    await page.getByRole('button', { name: 'Settings', exact: true }).click()
    const settings = page.getByRole('dialog', { name: 'Settings' })
    await expect(settings).toBeVisible()
    await settings.getByRole('tab', { name: 'Backups' }).click()

    const days = settings.getByRole('spinbutton', { name: 'Keep backups for N days' })
    await days.fill('7')
    const save = settings.getByRole('button', { name: 'Save changes' })
    await expect(save).toBeEnabled()
    await save.click()

    await expect(page.getByLabel('Notifications').getByText('Settings saved').last()).toBeVisible({ timeout: 60_000 })
    await expect.poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 }).toContain('max_days: 7')
  } finally {
    await harness.stop()
  }
})
