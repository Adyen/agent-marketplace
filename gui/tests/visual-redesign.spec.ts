import { execFileSync } from 'node:child_process'
import { writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { expect, test } from '@playwright/test'
import { distIndexExists, launchBuiltUserApp } from './app'
import { startHarness } from './harness'

// spec: gui-shell/update-shows-the-available-update-count
test('built GUI shows the resource update count after refresh', async ({ page }) => {
  expect(distIndexExists(), 'run gui-build before the Playwright suite').toBe(true)
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'update-availability' })
  const { bootstrap } = harness
  try {
    if (bootstrap.gitSourceWorking == null) throw new Error('update fixture has no working Git source')
    await launchBuiltUserApp(page, bootstrap)
    await expect(page.getByText('solo', { exact: true }).first()).toBeVisible({ timeout: 60_000 })
    const update = page.getByRole('button', { name: 'Update', exact: true })
    await expect(update.locator('.gui-update-count')).toHaveCount(0)

    const skillPath = join(bootstrap.gitSourceWorking, 'solo', 'SKILL.md')
    await writeFile(skillPath, '---\nname: solo\ndescription: Updated skill\n---\n# Updated skill\n')
    execFileSync('git', ['-C', bootstrap.gitSourceWorking, 'add', '.'])
    execFileSync('git', ['-C', bootstrap.gitSourceWorking, '-c', 'user.name=GUI test', '-c', 'user.email=gui@example.test', 'commit', '-m', 'update resource'])
    execFileSync('git', ['-C', bootstrap.gitSourceWorking, 'push', 'origin', 'main'])

    await page.keyboard.press('Meta+r')
    await expect(update).toContainText('1', { timeout: 60_000 })
    const toolbar = page.getByRole('toolbar', { name: 'Session actions' })
    const labels = await toolbar.locator('button').evaluateAll((buttons) =>
      buttons.map((button) => button.getAttribute('aria-label') ?? button.textContent?.trim() ?? ''),
    )
    expect(labels.indexOf('Update')).toBeLessThan(labels.indexOf('Sync'))
    await page.getByRole('separator', { name: 'Resize sidebar' }).press('End')
    await page.getByRole('separator', { name: 'Resize inspector' }).press('End')
  } finally {
    await harness.stop()
  }
})
