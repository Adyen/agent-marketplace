import { expect, test, type Locator } from '@playwright/test'
import { readFile, symlink, unlink } from 'node:fs/promises'
import { startHarness, type Bootstrap } from './harness'
import { distIndexExists, launchBuiltUserApp } from './app'
import { tabTo } from './keyboard'

const outsideSecret = 'outside-local-skill-private-bytes'

function requiredPath(value: string | undefined, label: string): string {
  if (value == null || value === '') {
    throw new Error(`local skill fixture did not provide ${label}`)
  }
  return value
}

async function captureProtected(bootstrap: Bootstrap): Promise<{ paths: string[]; bytes: Buffer[] }> {
  const paths = [
    requiredPath(bootstrap.localSkillFilesConfig, 'user config'),
    requiredPath(bootstrap.localSkillFilesLock, 'user lock'),
    requiredPath(bootstrap.localSkillFilesMaterialized, 'materialized file'),
  ]
  return { paths, bytes: await Promise.all(paths.map((path) => readFile(path))) }
}

async function expectProtectedUnchanged(before: { paths: string[]; bytes: Buffer[] }): Promise<void> {
  expect(await Promise.all(before.paths.map((path) => readFile(path)))).toEqual(before.bytes)
}

async function openLocalSkill(
  page: Parameters<typeof tabTo>[0],
  row: Locator,
  openWithButton: boolean,
  label: string,
): Promise<Locator> {
  await tabTo(page, row, `${label} row`, [])
  if (openWithButton) {
    await page.keyboard.press('Space')
    const button = page.getByRole('button', { name: 'Open resource', exact: true })
    await expect(button).toBeVisible()
    await button.click()
  } else {
    await page.keyboard.press('Enter')
  }
  const dialog = page.getByRole('dialog', { name: 'Resource inspection' })
  await expect(dialog).toBeVisible()
  return dialog
}

async function expectPrimaryAndSupport(
  page: Parameters<typeof tabTo>[0],
  dialog: Locator,
  primaryText: string,
  supportText: string,
): Promise<void> {
  const primary = dialog.getByRole('treeitem', { name: 'SKILL.md', exact: true })
  await expect(primary).toHaveAttribute('aria-selected', 'true')
  await expect(primary).toBeFocused()
  const preview = dialog.locator('pre')
  await expect(preview).toContainText(primaryText)
  await page.keyboard.press('ArrowUp')
  const support = dialog.getByRole('treeitem', { name: 'notes.md', exact: true })
  await expect(support).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(preview).toContainText(supportText)
}

async function closeInspection(page: Parameters<typeof tabTo>[0], dialog: Locator): Promise<void> {
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
}

// spec: gui-shell/directory-and-git-skills-open-read-only-in-the-inspector
test('built GUI opens directory and Git skills read-only with Enter and Open resource', async ({ page }) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the local skill files journey')
  }
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'local-skill-files' })
  try {
    await page.setViewportSize({ width: 1280, height: 800 })
    await launchBuiltUserApp(page, harness.bootstrap)
    const resources = page.getByRole('region', { name: 'Resources' })
    const directory = page.getByRole('option').filter({ hasText: 'directory-skill' })
    const git = page.getByRole('option').filter({ hasText: 'git-skill' })
    await expect(resources).toBeVisible({ timeout: 60_000 })
    await expect(directory).toBeVisible({ timeout: 30_000 })
    await expect(git).toBeVisible()

    const protectedFiles = await captureProtected(harness.bootstrap)
    let installedFileRequests = 0
    let marketplaceInspectionRequests = 0
    let mutationRequests = 0
    page.on('request', (request) => {
      const pathname = new URL(request.url()).pathname
      if (pathname === '/api/installed-skill-files') {
        installedFileRequests++
      } else if (pathname === '/api/inspection') {
        marketplaceInspectionRequests++
      } else if (pathname === '/api/actions' || pathname === '/api/configuration/actions') {
        mutationRequests++
      }
    })
    for (const item of [
      { row: directory, name: 'Directory skill', primary: 'Directory skill', support: 'Directory support' },
      { row: git, name: 'Git skill', primary: 'Git skill', support: 'Git support' },
    ]) {
      const byEnter = await openLocalSkill(page, item.row, false, item.name)
      await expectPrimaryAndSupport(page, byEnter, item.primary, item.support)
      await closeInspection(page, byEnter)
      await expectProtectedUnchanged(protectedFiles)

      const byButton = await openLocalSkill(page, item.row, true, item.name)
      await expectPrimaryAndSupport(page, byButton, item.primary, item.support)
      await closeInspection(page, byButton)
      await expectProtectedUnchanged(protectedFiles)
    }
    expect(installedFileRequests).toBe(8)
    expect(marketplaceInspectionRequests).toBe(0)
    expect(mutationRequests).toBe(0)
  } finally {
    await harness.stop()
  }
})

// spec: gui-shell/unsafe-or-missing-local-skill-source-fails-actionably
test('built GUI gives actionable copy and returns no outside bytes for an escaping local support link', async ({ page }) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the local skill files journey')
  }
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'local-skill-files' })
  try {
    const sourceRoot = requiredPath(harness.bootstrap.localSkillFilesDirectory, 'directory source root')
    const outsideFile = requiredPath(harness.bootstrap.localSkillFilesOutside, 'outside source file')
    const outsideContents = await readFile(outsideFile, 'utf8')
    const supportFile = `${sourceRoot}/directory-skill/notes.md`
    await unlink(supportFile)
    await symlink(outsideFile, supportFile)

    await page.setViewportSize({ width: 1280, height: 800 })
    await launchBuiltUserApp(page, harness.bootstrap)
    const row = page.getByRole('option').filter({ hasText: 'directory-skill' })
    await expect(row).toBeVisible({ timeout: 60_000 })
    const protectedFiles = await captureProtected(harness.bootstrap)
    const failedResponse = page.waitForResponse((response) =>
      new URL(response.url()).pathname === '/api/installed-skill-files' && response.status() === 502,
    )
    await tabTo(page, row, 'Directory skill row', [])
    await page.keyboard.press('Enter')
    const response = await failedResponse
    expect(response.status()).toBe(502)
    const body = await response.text()
    expect(body).not.toContain(outsideContents)
    const dialog = page.getByRole('dialog', { name: 'Resource inspection' })
    await expect(dialog.getByRole('alert')).toHaveText('Skill source files are missing or unsafe; fix the source and refresh.')
    await expect(page.locator('body')).not.toContainText(outsideContents)
    await expectProtectedUnchanged(protectedFiles)
    await closeInspection(page, dialog)
  } finally {
    await harness.stop()
  }
})
