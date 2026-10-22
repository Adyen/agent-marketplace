import { expect, test, type Locator, type Page } from '@playwright/test'
import { existsSync, readdirSync } from 'node:fs'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { startHarness } from './harness'
import { distIndexExists, serveBuiltApp } from './app'

async function actionVisibility(page: Page, dialog: Locator, action: Locator, name: string) {
  await action.focus()
  const [dialogBox, actionBox] = await Promise.all([dialog.boundingBox(), action.boundingBox()])
  const viewport = page.viewportSize()
  const hittable = await action.evaluate((element) => {
    const box = element.getBoundingClientRect()
    const target = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2)
    return target != null && (target === element || element.contains(target))
  })
  const insideDialog = dialogBox != null && actionBox != null &&
    actionBox.x >= dialogBox.x && actionBox.y >= dialogBox.y &&
    actionBox.x + actionBox.width <= dialogBox.x + dialogBox.width &&
    actionBox.y + actionBox.height <= dialogBox.y + dialogBox.height
  const insideViewport = viewport != null && actionBox != null &&
    actionBox.x >= 0 && actionBox.y >= 0 &&
    actionBox.x + actionBox.width <= viewport.width && actionBox.y + actionBox.height <= viewport.height
  return { name, dialogBox, actionBox, viewport, insideDialog, insideViewport, hittable }
}

async function sourceListVisibility(page: Page, dialog: Locator, sources: Locator, longLocation: string) {
  const item = sources.locator('li').filter({ hasText: longLocation })
  const kind = item.getByText('DIRECTORY', { exact: true })
  const location = item.getByText(longLocation, { exact: true })
  const [dialogBox, kindBox, locationBox] = await Promise.all([
    dialog.boundingBox(), kind.boundingBox(), location.boundingBox(),
  ])
  const textRects = await location.evaluate((element) => {
    const range = document.createRange()
    range.selectNodeContents(element)
    return Array.from(range.getClientRects()).map((rect) => ({
      left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom,
    }))
  })
  const [dialogWidth, sourcesWidth, documentWidth] = await Promise.all([
    dialog.evaluate((element) => [element.scrollWidth, element.clientWidth]),
    sources.evaluate((element) => [element.scrollWidth, element.clientWidth]),
    page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]),
  ])
  const viewport = page.viewportSize()
  return {
    kindOnLocationRow: kindBox != null && locationBox != null && kindBox.y < locationBox.y + locationBox.height && locationBox.y < kindBox.y + kindBox.height,
    locationLines: new Set(textRects.map((rect) => Math.round(rect.top))).size,
    locationWithinDialog: dialogBox != null && viewport != null && textRects.length > 0 && textRects.every((rect) =>
      rect.left >= dialogBox.x && rect.right <= dialogBox.x + dialogBox.width &&
      rect.left >= 0 && rect.right <= viewport.width && rect.top >= dialogBox.y && rect.bottom <= dialogBox.y + dialogBox.height,
    ),
    dialogHasNoHorizontalOverflow: dialogWidth[0] <= dialogWidth[1],
    sourcesHaveNoHorizontalOverflow: sourcesWidth[0] <= sourcesWidth[1],
    pageHasNoHorizontalOverflow: documentWidth[0] <= documentWidth[1],
  }
}

// spec: gui-configuration/add-and-remove-user-sources
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
    await expect(page.getByText('solo', { exact: true }).first()).toBeVisible({ timeout: 60_000 })

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
    const dialog = page.getByRole('dialog', { name: 'Settings' })
    await expect(dialog).toBeVisible()
    const notifications = page.getByLabel('Notifications')

    const addSection = dialog.getByLabel('Add user source')
    await expect(addSection).toBeVisible()
    await expect(addSection.getByRole('group', { name: 'Source kind' }).getByRole('radio', { name: 'Directory registry' })).toBeChecked()
    await expect(addSection.getByLabel('Source location')).toBeEnabled()
    await addSection.getByLabel('Source location').pressSequentially(sourceDir)
    await dialog.getByRole('button', { name: 'Close dialog', exact: true }).press('Enter')
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
    await expect(userSources).toContainText('DIRECTORY')
    await expect(notifications.getByText('mutation complete')).toBeVisible()
    await expect(dialog.getByText('Applying changes…')).toHaveCount(0)
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
    await expect(dialog.getByText('Applying changes…')).toHaveCount(0)
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
      { status: 401, code: 'localapi.unauthorized' },
    ])
    expect(await readFile(userConfigPath, 'utf8')).toBe(original)
    expect(await readFile(fragmentPath, 'utf8')).toBe(fragment)
  } finally {
    await harness.stop()
  }
})

// spec: gui-configuration/add-and-remove-user-sources
// spec: gui-configuration/removing-a-marketplace-endpoint-registration-changes-only-that-entry
test('built GUI adds and removes a Marketplace endpoint registration in the open Sources dialog', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }
  await page.setViewportSize({ width: 1280, height: 720 })

  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'marketplace' })
  const { bootstrap } = harness
  const endpoint = bootstrap.marketplaceEndpoint
  const artifactDirectory = process.env.AM_MARKETPLACE_REMOVAL_EVIDENCE_DIR
  const artifactPath = (name: string) =>
    artifactDirectory == null ? testInfo.outputPath(name) : join(artifactDirectory, name)
  const formActionScreenshotPath = artifactPath('marketplace-removal-form-action.png')
  const marketplacePresentScreenshotPath = artifactPath('marketplace-removal-present.png')
  const marketplaceRemovedScreenshotPath = artifactPath('marketplace-removal-removed.png')

  try {
    if (endpoint == null) {
      throw new Error('Marketplace endpoint fixture missing from bootstrap')
    }
    const home = bootstrap.home!
    const userConfigPath = join(home, '.agents', 'config.yaml')
    const fragmentPath = join(home, '.agents', 'fragment.yaml')
    const orgConfigPath = bootstrap.orgConfig!
    const repoConfigPath = join(bootstrap.repo!, '.agents', 'config.yaml')
    const repoLocalPath = join(bootstrap.repo!, '.agents', 'config.local.yaml')
    const codexHome = join(home, '.codex')
    const xdgDirs = [bootstrap.xdgConfig!, bootstrap.xdgState!, bootstrap.xdgCache!]

    await serveBuiltApp(page, bootstrap.url)
    await page.addInitScript(
      ([credential]) => {
        ;(window as unknown as Record<string, unknown>).__AM_GUI_BOOTSTRAP__ = { credential }
      },
      [bootstrap.credential] as const,
    )
    await page.goto(bootstrap.url, { waitUntil: 'domcontentloaded' })
    await expect(page.getByText('solo', { exact: true }).first()).toBeVisible({ timeout: 60_000 })

    const original = await readFile(userConfigPath, 'utf8')
    const fragment = await readFile(fragmentPath, 'utf8')
    const orgBefore = await readFile(orgConfigPath, 'utf8')
    const repoBefore = await readFile(repoConfigPath, 'utf8')
    const repoLocalBefore = await readFile(repoLocalPath, 'utf8')
    const sourcesButton = page.getByRole('button', { name: 'Sources' })
    await sourcesButton.press('Enter')
    const dialog = page.getByRole('dialog', { name: 'Settings' })
    await expect(dialog).toBeVisible()

    const addSection = dialog.getByLabel('Add user source')
    const userSources = dialog.getByLabel('User sources')
    const longLocation = join(home, 'keep-source')
    await expect(userSources).toContainText(longLocation)

    const listMetrics = await sourceListVisibility(page, dialog, userSources, longLocation)

    const kindGroup = addSection.getByRole('group', { name: 'Source kind' })
    await expect(kindGroup).toBeVisible()
    const marketplaceRadio = kindGroup.getByRole('radio', { name: 'Marketplace endpoint' })
    await marketplaceRadio.focus()
    await marketplaceRadio.press('Space')
    await expect(marketplaceRadio).toBeChecked()
    await expect(addSection.getByLabel('Source ref')).toHaveCount(0)
    await expect(addSection.getByLabel('Source subpath')).toHaveCount(0)
    const location = addSection.getByLabel('Source location')
    await location.pressSequentially(endpoint)
    const addButton = addSection.getByRole('button', { name: 'Add source' })
    const cancelButton = dialog.getByRole('button', { name: 'Close dialog', exact: true })
    await expect(addButton).toBeEnabled()
    const cancelMetrics = await actionVisibility(page, dialog, cancelButton, 'Close dialog')
    const addMetrics = await actionVisibility(page, dialog, addButton, 'Add source')
    await page.screenshot({ path: formActionScreenshotPath, fullPage: true })
    const viewport = page.viewportSize()
    const visibility = {
      viewport,
      list: listMetrics,
      actions: [cancelMetrics, addMetrics],
    }
    expect(visibility, `visibility metrics: ${JSON.stringify(visibility)}`).toMatchObject({
      viewport: { width: 1280, height: 720 },
      list: {
        kindOnLocationRow: true,
        locationWithinDialog: true,
        dialogHasNoHorizontalOverflow: true,
        sourcesHaveNoHorizontalOverflow: true,
        pageHasNoHorizontalOverflow: true,
      },
      actions: [
        { name: 'Close dialog', insideDialog: true, insideViewport: true, hittable: true },
        { name: 'Add source', insideDialog: true, insideViewport: true, hittable: true },
      ],
    })
    expect(listMetrics.locationLines).toBeGreaterThan(1)

    await cancelButton.press('Enter')
    await expect(dialog).not.toBeVisible()
    expect(await readFile(userConfigPath, 'utf8')).toBe(original)

    await sourcesButton.press('Enter')
    await expect(dialog).toBeVisible()
    await expect(marketplaceRadio).toBeEnabled()
    await marketplaceRadio.focus()
    await marketplaceRadio.press('Space')
    await expect(marketplaceRadio).toBeChecked()
    await expect(location).toHaveValue('')
    await location.pressSequentially(endpoint)
    await expect(addButton).toBeEnabled()
    const finalAddMetrics = await actionVisibility(page, dialog, addButton, 'Add source')
    expect(finalAddMetrics).toMatchObject({ insideDialog: true, insideViewport: true, hittable: true })
    await addButton.press('Enter')

    await expect(userSources).toContainText(endpoint, { timeout: 60_000 })
    await expect(userSources).toContainText('MARKETPLACE')
    await expect(page.getByLabel('Notifications').getByText('mutation complete')).toBeVisible()
    await expect(dialog.getByText('Applying changes…')).toHaveCount(0)
    await expect(addButton).toBeEnabled()
    await expect(location).toBeEnabled()
    await expect(dialog).toBeVisible()
    await userSources.scrollIntoViewIfNeeded()
    await userSources.screenshot({ path: marketplacePresentScreenshotPath })
    const updatedListMetrics = await sourceListVisibility(page, dialog, userSources, longLocation)
    expect(updatedListMetrics).toMatchObject({
      kindOnLocationRow: true,
      locationWithinDialog: true,
      dialogHasNoHorizontalOverflow: true,
      sourcesHaveNoHorizontalOverflow: true,
      pageHasNoHorizontalOverflow: true,
    })
    expect(updatedListMetrics.locationLines).toBeGreaterThan(1)

    const expectedRoot = original.replace(
      'resources:\n',
      `  marketplace:\n    marketplace: ${endpoint}\nresources:\n`,
    )
    expect(expectedRoot).not.toBe(original)
    await expect.poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 }).toBe(expectedRoot)
    expect(await readFile(fragmentPath, 'utf8')).toBe(fragment)
    expect(await readFile(orgConfigPath, 'utf8')).toBe(orgBefore)
    expect(await readFile(repoConfigPath, 'utf8')).toBe(repoBefore)
    expect(await readFile(repoLocalPath, 'utf8')).toBe(repoLocalBefore)
    expect(existsSync(codexHome)).toBe(false)
    expect(existsSync(join(home, '.devin'))).toBe(false)
    expect(
      xdgDirs.map((dir) => (existsSync(dir) ? readdirSync(dir).sort().join(',') : '<absent>')),
    ).not.toContain('devin')

    const removeButton = userSources.getByRole('button', { name: `Remove source ${endpoint}` })
    await expect(removeButton).toBeVisible({ timeout: 5_000 })
    const removeMetrics = await actionVisibility(page, dialog, removeButton, 'Remove source')
    expect(removeMetrics).toMatchObject({ insideDialog: true, insideViewport: true, hittable: true })
    const confirmation = dialog.getByRole('region', { name: 'Confirm removal' })
    const notifications = page.getByLabel('Notifications')
    await expect(notifications.getByText('mutation complete')).toHaveCount(0, { timeout: 10_000 })

    await removeButton.press('Enter')
    await expect(confirmation).toBeVisible()
    const cancelRemove = dialog.getByRole('button', { name: 'Cancel remove' })
    const cancelRemoveMetrics = await actionVisibility(page, dialog, cancelRemove, 'Cancel remove')
    expect(cancelRemoveMetrics).toMatchObject({ insideDialog: true, insideViewport: true, hittable: true })
    await cancelRemove.press('Enter')
    await expect(confirmation).toHaveCount(0)
    await expect(userSources).toContainText(endpoint)
    expect(await readFile(userConfigPath, 'utf8')).toBe(expectedRoot)

    await removeButton.press('Enter')
    await expect(confirmation).toBeVisible()
    const confirmRemove = dialog.getByRole('button', { name: 'Confirm remove' })
    const confirmRemoveMetrics = await actionVisibility(page, dialog, confirmRemove, 'Confirm remove')
    expect(confirmRemoveMetrics).toMatchObject({ insideDialog: true, insideViewport: true, hittable: true })
    await confirmRemove.press('Enter')

    await expect(userSources).not.toContainText(endpoint, { timeout: 60_000 })
    await expect(notifications.getByText('mutation complete').last()).toBeVisible({ timeout: 60_000 })
    await expect(dialog.getByText('Applying changes…')).toHaveCount(0)
    await expect(addButton).toBeEnabled()
    await expect(location).toBeEnabled()
    await expect(dialog).toBeVisible()
    await expect.poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 }).toBe(original)
    expect(await readFile(fragmentPath, 'utf8')).toBe(fragment)
    expect(await readFile(orgConfigPath, 'utf8')).toBe(orgBefore)
    expect(await readFile(repoConfigPath, 'utf8')).toBe(repoBefore)
    expect(await readFile(repoLocalPath, 'utf8')).toBe(repoLocalBefore)
    expect(existsSync(codexHome)).toBe(false)
    expect(existsSync(join(home, '.devin'))).toBe(false)
    expect(
      xdgDirs.map((dir) => (existsSync(dir) ? readdirSync(dir).sort().join(',') : '<absent>')),
    ).not.toContain('devin')
    const removedListMetrics = await sourceListVisibility(page, dialog, userSources, longLocation)
    expect(removedListMetrics).toMatchObject({
      kindOnLocationRow: true,
      locationWithinDialog: true,
      dialogHasNoHorizontalOverflow: true,
      sourcesHaveNoHorizontalOverflow: true,
      pageHasNoHorizontalOverflow: true,
    })
    expect(removedListMetrics.locationLines).toBeGreaterThan(1)
    await userSources.scrollIntoViewIfNeeded()
    await userSources.screenshot({ path: marketplaceRemovedScreenshotPath })
  } finally {
    await harness.stop()
  }
})

// spec: gui-configuration/add-and-remove-user-sources
// spec: gui-configuration/adding-a-git-or-directory-source-alone-changes-only-that-entry
test('built GUI adds and removes a user Git source with ref and subpath by keyboard', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }
  await page.setViewportSize({ width: 1280, height: 720 })

  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'sources-git' })
  const { bootstrap } = harness
  const artifactDirectory = process.env.AM_GIT_SOURCE_EVIDENCE_DIR
  const artifactPath = (name: string) =>
    artifactDirectory == null ? testInfo.outputPath(name) : join(artifactDirectory, name)

  try {
    if (bootstrap.gitSourceURL == null) {
      throw new Error('Git source fixture missing from bootstrap')
    }
    if (bootstrap.codexHome == null) {
      throw new Error('CODEX_HOME fixture missing from bootstrap')
    }
    const home = bootstrap.home!
    const gitURL = bootstrap.gitSourceURL
    const userConfigPath = join(home, '.agents', 'config.yaml')
    const fragmentPath = join(home, '.agents', 'fragment.yaml')
    const lockPath = join(home, '.agents', 'lock.yaml')
    const orgConfigPath = bootstrap.orgConfig!
    const repoConfigPath = join(bootstrap.repo!, '.agents', 'config.yaml')
    const repoLocalPath = join(bootstrap.repo!, '.agents', 'config.local.yaml')
    const codexHome = bootstrap.codexHome
    const xdgDirs = [bootstrap.xdgConfig!, bootstrap.xdgState!, bootstrap.xdgCache!]

    await serveBuiltApp(page, bootstrap.url)
    await page.addInitScript(
      ([credential]) => {
        ;(window as unknown as Record<string, unknown>).__AM_GUI_BOOTSTRAP__ = { credential }
      },
      [bootstrap.credential] as const,
    )
    await page.goto(bootstrap.url, { waitUntil: 'domcontentloaded' })
    await expect(page.getByText('solo', { exact: true }).first()).toBeVisible({ timeout: 60_000 })

    const original = await readFile(userConfigPath, 'utf8')
    const fragment = await readFile(fragmentPath, 'utf8')
    const orgBefore = await readFile(orgConfigPath, 'utf8')
    const repoBefore = await readFile(repoConfigPath, 'utf8')
    const repoLocalBefore = await readFile(repoLocalPath, 'utf8')
    const keepDir = join(home, 'keep-source')
    const addedRoot =
      `apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\n` +
      `logging: {level: debug}\nsources:\n  keep: {directory: ${keepDir}}\n` +
      `  repo:\n    git: ${gitURL}\n    ref: main\n    path: solo\n` +
      `resources:\n  keep: {all: true, harnesses: [devin]}\n`

    const sourcesButton = page.getByRole('button', { name: 'Sources' })
    await sourcesButton.press('Enter')
    const dialog = page.getByRole('dialog', { name: 'Settings' })
    await expect(dialog).toBeVisible()
    const notifications = page.getByLabel('Notifications')

    const managedSection = dialog.getByLabel('Managed outside the GUI')
    await expect(managedSection.getByText('Included files, per-source configuration, and other settings are managed outside the GUI.')).toBeVisible()
    const orgSection = dialog.getByLabel('Organization sources')
    await expect(orgSection.getByText('Managed by your organization; read-only.')).toBeVisible()
    await expect(orgSection).toContainText('DIRECTORY')
    await expect(orgSection.locator('button, input, select, a')).toHaveCount(0)
    await page.screenshot({ path: artifactPath('git-source-before.png'), fullPage: true })

    const addSection = dialog.getByLabel('Add user source')
    const kindGroup = addSection.getByRole('group', { name: 'Source kind' })
    const gitRadio = kindGroup.getByRole('radio', { name: 'Git registry' })
    await expect(gitRadio).toBeEnabled()
    await gitRadio.focus()
    await gitRadio.press('Space')
    await expect(gitRadio).toBeChecked()

    const location = addSection.getByLabel('Source location')
    const ref = addSection.getByLabel('Source ref')
    const subpath = addSection.getByLabel('Source subpath')
    await expect(location).toBeEnabled()
    await location.pressSequentially(gitURL)
    await ref.pressSequentially('main')
    await subpath.pressSequentially('solo')
    const addButton = addSection.getByRole('button', { name: 'Add source' })
    await expect(addButton).toBeEnabled()
    const gitControls = [
      await actionVisibility(page, dialog, ref, 'Source ref'),
      await actionVisibility(page, dialog, subpath, 'Source subpath'),
      await actionVisibility(page, dialog, addButton, 'Add source'),
    ]
    for (const metrics of gitControls) {
      expect(metrics, `git control ${metrics.name}: ${JSON.stringify(metrics)}`).toMatchObject({
        insideDialog: true,
        insideViewport: true,
        hittable: true,
      })
    }
    await addSection.screenshot({ path: artifactPath('git-source-form-action.png') })
    await addButton.press('Enter')

    const userSources = dialog.getByLabel('User sources')
    await expect(userSources).toContainText(gitURL, { timeout: 60_000 })
    await expect(userSources).toContainText('GIT')
    await expect(userSources).toContainText('main · solo')
    await expect(notifications.getByText('mutation complete')).toBeVisible()
    await expect(dialog.getByText('Applying changes…')).toHaveCount(0)
    await expect(addButton).toBeEnabled()
    await expect(location).toBeEnabled()
    await expect(dialog).toBeVisible()
    await expect(managedSection.getByText('Included files, per-source configuration, and other settings are managed outside the GUI.')).toBeVisible()
    await expect.poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 }).toBe(addedRoot)
    expect(await readFile(fragmentPath, 'utf8')).toBe(fragment)
    expect(await readFile(orgConfigPath, 'utf8')).toBe(orgBefore)
    expect(await readFile(repoConfigPath, 'utf8')).toBe(repoBefore)
    expect(await readFile(repoLocalPath, 'utf8')).toBe(repoLocalBefore)
    expect(existsSync(lockPath)).toBe(true)
    expect(existsSync(codexHome)).toBe(false)
    expect(existsSync(join(home, '.devin'))).toBe(false)
    expect(existsSync(join(bootstrap.xdgConfig!, 'devin'))).toBe(false)
    expect(
      xdgDirs.map((dir) => (existsSync(dir) ? readdirSync(dir).sort().join(',') : '<absent>')).join(','),
    ).not.toContain('devin')
    await page.screenshot({ path: artifactPath('git-source-added.png'), fullPage: true })

    const removeButton = userSources.getByRole('button', { name: `Remove source ${gitURL}` })
    await expect(removeButton).toBeEnabled()
    await removeButton.press('Enter')
    const confirmation = dialog.getByRole('region', { name: 'Confirm removal' })
    await expect(confirmation).toBeVisible()
    const cancelRemove = dialog.getByRole('button', { name: 'Cancel remove' })
    await cancelRemove.press('Enter')
    await expect(confirmation).toHaveCount(0)
    expect(await readFile(userConfigPath, 'utf8')).toBe(addedRoot)
    await expect(userSources).toContainText(gitURL)

    await removeButton.press('Enter')
    await expect(confirmation).toBeVisible()
    const confirmRemove = dialog.getByRole('button', { name: 'Confirm remove' })
    await expect(confirmRemove).toBeEnabled()
    await confirmRemove.press('Enter')

    await expect(userSources.getByText(gitURL)).toHaveCount(0, { timeout: 60_000 })
    await expect(userSources).toContainText(keepDir)
    await expect(notifications.getByText('mutation complete').last()).toBeVisible({ timeout: 60_000 })
    await expect(dialog.getByText('Applying changes…')).toHaveCount(0)
    await expect(dialog).toBeVisible()
    await expect.poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 }).toBe(original)
    expect(await readFile(fragmentPath, 'utf8')).toBe(fragment)
    expect(await readFile(orgConfigPath, 'utf8')).toBe(orgBefore)
    expect(await readFile(repoConfigPath, 'utf8')).toBe(repoBefore)
    expect(await readFile(repoLocalPath, 'utf8')).toBe(repoLocalBefore)
    expect(existsSync(codexHome)).toBe(false)
    expect(existsSync(join(home, '.devin'))).toBe(false)
    expect(existsSync(join(bootstrap.xdgConfig!, 'devin'))).toBe(false)
    expect(
      xdgDirs.map((dir) => (existsSync(dir) ? readdirSync(dir).sort().join(',') : '<absent>')).join(','),
    ).not.toContain('devin')
    await page.screenshot({ path: artifactPath('git-source-removed.png'), fullPage: true })
  } finally {
    await harness.stop()
  }
})
