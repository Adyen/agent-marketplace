import { test, expect } from '@playwright/test'
import { existsSync, readdirSync } from 'node:fs'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { startHarness } from './harness'
import { distIndexExists, serveBuiltApp } from './app'

// spec: gui-configuration/adding-a-git-or-directory-source-alone-changes-only-that-entry
test('built GUI adds and removes a user directory source against the real user-scope core', async ({
  page,
}) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }

  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'sources' })
  const { bootstrap } = harness
  const home = bootstrap.home!
  const sourceDir = bootstrap.addSourceDir!
  const userConfigPath = join(home, '.agents', 'config.yaml')
  const fragmentPath = join(home, '.agents', 'fragment.yaml')
  const orgConfigPath = bootstrap.orgConfig!
  const repoConfigPath = join(bootstrap.repo!, '.agents', 'config.yaml')
  const repoLocalPath = join(bootstrap.repo!, '.agents', 'config.local.yaml')
  const codexHome = join(home, '.codex')
  const xdgDirs = [bootstrap.xdgConfig!, bootstrap.xdgState!, bootstrap.xdgCache!]

  try {
    await serveBuiltApp(page, bootstrap.url)
    await page.addInitScript(
      ([credential]) => {
        ;(window as unknown as Record<string, unknown>).__AM_GUI_BOOTSTRAP__ = { credential }
      },
      [bootstrap.credential] as const,
    )
    await page.goto(bootstrap.url, { waitUntil: 'domcontentloaded' })
    await expect(page.getByText('skill/solo').first()).toBeVisible({ timeout: 60_000 })

    const original = await readFile(userConfigPath, 'utf8')
    const fragment = await readFile(fragmentPath, 'utf8')
    const orgBefore = await readFile(orgConfigPath, 'utf8')
    const repoBefore = await readFile(repoConfigPath, 'utf8')
    const repoLocalBefore = await readFile(repoLocalPath, 'utf8')
    const addedRoot =
      `apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\n` +
      `sources:\n  keep: {directory: ${join(home, 'keep-source')}}\n` +
      `  added-source:\n    directory: ${sourceDir}\nresources:\n  keep: {all: true}\n`

    const sourcesButton = page.getByRole('button', { name: 'Sources' })
    await sourcesButton.press('Enter')
    const dialog = page.getByRole('dialog', { name: 'Sources' })
    await expect(dialog).toBeVisible()
    const notifications = page.getByLabel('Notifications')

    const addSection = dialog.getByLabel('Add user source')
    await expect(addSection).toBeVisible()
    await expect(addSection.getByLabel('Source location')).toBeEnabled()
    await addSection.getByLabel('Source location').pressSequentially(sourceDir)
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).press('Enter')
    await expect(dialog).not.toBeVisible()
    expect(await readFile(userConfigPath, 'utf8')).toBe(original)

    await sourcesButton.press('Enter')
    await expect(dialog).toBeVisible()
    const location = addSection.getByLabel('Source location')
    await expect(location).toBeEnabled({ timeout: 60_000 })
    await expect(location).toHaveValue('')
    await location.pressSequentially(sourceDir)
    const addButton = addSection.getByRole('button', { name: 'Add source' })
    await expect(addButton).toBeEnabled({ timeout: 60_000 })
    await addButton.press('Enter')

    const userSources = dialog.getByLabel('User sources')
    await expect(userSources).toContainText(sourceDir, { timeout: 60_000 })
    await expect(userSources).toContainText('Directory registry')
    await expect(notifications.getByText('mutation complete')).toBeVisible()
    await expect(dialog.getByText('mutation running')).toHaveCount(0)
    await expect(addButton).toBeEnabled()
    await expect(location).toBeEnabled()
    await expect(dialog).toBeVisible()
    await expect
      .poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 })
      .toBe(addedRoot)
    expect(await readFile(fragmentPath, 'utf8')).toBe(fragment)
    expect(await readFile(orgConfigPath, 'utf8')).toBe(orgBefore)

    const remove = userSources.getByRole('button', { name: `Remove source ${sourceDir}` })
    await expect(remove).toBeEnabled()
    await remove.press('Enter')
    const cancelRemove = dialog.getByRole('button', { name: 'Cancel remove' })
    await expect(cancelRemove).toBeEnabled()
    await cancelRemove.press('Enter')
    expect(await readFile(userConfigPath, 'utf8')).toBe(addedRoot)

    await expect(remove).toBeEnabled()
    await remove.press('Enter')
    const confirmRemove = dialog.getByRole('button', { name: 'Confirm remove' })
    await expect(confirmRemove).toBeEnabled()
    await confirmRemove.press('Enter')

    await expect(userSources.getByText(sourceDir)).toHaveCount(0, { timeout: 60_000 })
    await expect(notifications.getByText('mutation complete').last()).toBeVisible()
    await expect(dialog.getByText('mutation running')).toHaveCount(0)
    await expect(addButton).toBeEnabled()
    await expect(location).toBeEnabled()
    await expect(dialog).toBeVisible()
    await expect
      .poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 })
      .toBe(original)

    expect(await readFile(fragmentPath, 'utf8')).toBe(fragment)
    expect(await readFile(orgConfigPath, 'utf8')).toBe(orgBefore)
    expect(await readFile(repoConfigPath, 'utf8')).toBe(repoBefore)
    expect(await readFile(repoLocalPath, 'utf8')).toBe(repoLocalBefore)
    expect(existsSync(codexHome)).toBe(false)
    expect(existsSync(join(bootstrap.xdgConfig!, 'devin'))).toBe(false)
    expect(
      xdgDirs.map((dir) => (existsSync(dir) ? readdirSync(dir).sort().join(',') : '<absent>')),
    ).not.toContain('devin')

    const rejections = await page.evaluate(
      async ({ url, credential }) => {
        const post = async (body: string, token = credential) => {
          const response = await fetch(`${url}/api/configuration/actions`, {
            method: 'POST',
            headers: { 'X-Agent-Marketplace-Token': token, 'Content-Type': 'application/json' },
            body,
          })
          const parsed = (await response.json().catch(() => ({}))) as { code?: string }
          return { status: response.status, code: parsed.code ?? null }
        }
        return Promise.all([
          post('{"action":"add-source"}'),
          post('{"action":"remove-source"}'),
          post(`{"action":"remove-source","id":"${'0'.repeat(64)}"}`),
          post('{"action":"add-source","source":{"kind":"marketplace","location":"https://m.example"}}'),
          post('{"action":"add-source","source":{"kind":"directory","location":"relative/dir"}}'),
          post('{"action":"add-source","source":{"kind":"directory","location":"/tmp/x","ref":null}}'),
          post('{"action":"remove-source","id":"x"}', 'wrong'),
        ])
      },
      { url: bootstrap.url, credential: bootstrap.credential },
    )
    expect(rejections).toEqual([
      { status: 400, code: 'localapi.invalid_request' },
      { status: 400, code: 'localapi.invalid_request' },
      { status: 400, code: 'localapi.invalid_request' },
      { status: 400, code: 'localapi.invalid_request' },
      { status: 400, code: 'localapi.invalid_request' },
      { status: 400, code: 'localapi.invalid_request' },
      { status: 401, code: 'localapi.unauthorized' },
    ])
    expect(await readFile(userConfigPath, 'utf8')).toBe(original)
    expect(await readFile(fragmentPath, 'utf8')).toBe(fragment)
  } finally {
    await harness.stop()
  }
})
