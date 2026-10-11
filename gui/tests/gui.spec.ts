import { test, expect, type Locator, type Page, type TestInfo } from '@playwright/test'
import { existsSync, mkdirSync, readdirSync } from 'node:fs'
import { readFile } from 'node:fs/promises'
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

async function focusAndCheckControl(page: Page, dialog: Locator, control: Locator) {
  await control.scrollIntoViewIfNeeded()
  await control.focus()
  await expect(control).toBeFocused()
  const [box, dialogBox] = await Promise.all([control.boundingBox(), dialog.boundingBox()])
  const viewport = page.viewportSize()
  expect(box).not.toBeNull()
  expect(dialogBox).not.toBeNull()
  expect(viewport).not.toBeNull()
  if (box == null || dialogBox == null || viewport == null) {
    throw new Error('focused control has no measurable geometry')
  }
  expect(
    box.x >= 0 && box.y >= 0 &&
      box.x + box.width <= viewport.width && box.y + box.height <= viewport.height,
  ).toBe(true)
  expect(
    box.x >= dialogBox.x && box.y >= dialogBox.y &&
      box.x + box.width <= dialogBox.x + dialogBox.width &&
      box.y + box.height <= dialogBox.y + dialogBox.height,
  ).toBe(true)
  const hittable = await control.evaluate((element) => {
    const box = element.getBoundingClientRect()
    const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2)
    return hit != null && (hit === element || element.contains(hit))
  })
  expect(hittable).toBe(true)
}

function firstRunArtifactPath(testInfo: TestInfo, name: string): string {
  const directory = process.env.AM_WORK23_EVIDENCE_DIR
  return directory == null ? testInfo.outputPath(name) : join(directory, name)
}

function observeConfigurationActions(page: Page): string[] {
  const bodies: string[] = []
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/configuration/actions') {
      bodies.push(request.postData() ?? '')
    }
  })
  return bodies
}

test('built GUI drives the real user-scope core', async ({ page }) => {
  if (!distIndexExists()) {
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
// spec: gui-configuration/organization-sources-are-read-only
// spec: gui-configuration/harness-target-edit-matches-the-cli-mutation
// spec: session-interactions/toggling-harness-targets-does-not-write
// spec: session-interactions/save-selected-harnesses
// spec: session-interactions/empty-selection
test('built GUI saves Harness targets through the real user-scope core', async ({ page }) => {
  if (!distIndexExists()) {
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
    const orgSources = dialog.getByLabel('Organization sources')
    await expect(orgSources).toContainText('Directory registry')
    await expect(orgSources).toContainText('Managed by your organization; read-only.')
    await expect(orgSources.getByRole('button')).toHaveCount(0)
    await expect(dialog.getByLabel('User sources')).toContainText('Directory registry')
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
// spec: gui-first-run/optional-source-step-uses-the-configuration-form
test('built GUI guided first-run setup initializes the absent root by keyboard', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }
  await page.setViewportSize({ width: 1280, height: 720 })

  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'first-run' })
  const { bootstrap } = harness
  const home = bootstrap.home!
  const repo = bootstrap.repo!
  const userConfigPath = join(home, '.agents', 'config.yaml')
  const userLockPath = join(home, '.agents', 'lock.yaml')
  const repoConfigPath = join(repo, '.agents', 'config.yaml')
  const repoLocalPath = join(repo, '.agents', 'config.local.yaml')
  const repoConfigBefore = await readFile(repoConfigPath)
  const repoLocalBefore = await readFile(repoLocalPath)
  const xdgDirs = [bootstrap.xdgConfig!, bootstrap.xdgState!, bootstrap.xdgCache!]
  const artifactPath = (name: string) => firstRunArtifactPath(testInfo, name)

  const xdgEntries = () =>
    xdgDirs.map((dir) => (existsSync(dir) ? readdirSync(dir).sort().join(',') : '<absent>'))

  try {
    expect(existsSync(userConfigPath)).toBe(false)
    expect(existsSync(userLockPath)).toBe(false)

    const actionBodies = observeConfigurationActions(page)
    await launchBuiltUserApp(page, bootstrap)

    const dialog = page.getByRole('dialog', { name: 'Set up Agent Marketplace' })
    await expect(dialog).toBeVisible({ timeout: 60_000 })

    const factory = dialog.getByRole('checkbox', { name: 'Target factory' })
    const devin = dialog.getByRole('checkbox', { name: 'Target devin' })
    await expect(factory).toBeChecked()
    await expect(devin).not.toBeChecked()

    await focusAndCheckControl(page, dialog, factory)
    await page.keyboard.press('Space')
    await expect(factory).not.toBeChecked()
    await expect(dialog.getByRole('button', { name: 'Next' })).toBeDisabled()
    await page.keyboard.press('Space')
    await expect(factory).toBeChecked()
    await focusAndCheckControl(page, dialog, devin)
    await page.keyboard.press('Space')
    await expect(devin).toBeChecked()

    await page.keyboard.press('Escape')
    await expect(dialog).not.toBeVisible()
    expect(existsSync(userConfigPath)).toBe(false)
    expect(existsSync(userLockPath)).toBe(false)
    await page.waitForTimeout(300)
    await expect(dialog).not.toBeVisible()
    expect(existsSync(userConfigPath)).toBe(false)
    expect((await readFile(repoConfigPath)).equals(repoConfigBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    expect(xdgEntries()).toEqual(['<absent>', '<absent>', '<absent>'])

    const setupButton = page.getByRole('button', { name: 'Set up' })
    await expect(setupButton).toBeVisible()
    await focusAndCheckControl(page, page.locator('body'), setupButton)
    await page.keyboard.press('Enter')
    await expect(dialog).toBeVisible()
    await expect(factory).toBeChecked()
    await expect(devin).not.toBeChecked()

    await focusAndCheckControl(page, dialog, dialog.getByRole('button', { name: 'Next' }))
    await page.keyboard.press('Enter')

    const no = dialog.getByRole('radio', { name: /no/i })
    const yes = dialog.getByRole('radio', { name: /yes/i })
    await expect(dialog.getByRole('button', { name: 'Finish setup' })).toHaveCount(0)
    await expect(dialog.getByRole('button', { name: 'Next' })).toBeDisabled()
    await expect(dialog).toContainText('basic telemetry')
    await expect(no).not.toBeChecked()
    await expect(yes).not.toBeChecked()

    await focusAndCheckControl(page, dialog, dialog.getByRole('button', { name: 'Back' }))
    await page.keyboard.press('Enter')
    await expect(factory).toBeChecked()
    await focusAndCheckControl(page, dialog, dialog.getByRole('button', { name: 'Next' }))
    await page.keyboard.press('Enter')
    await focusAndCheckControl(page, dialog, yes)
    await page.keyboard.press('Space')
    await expect(yes).toBeChecked()
    expect(existsSync(userConfigPath)).toBe(false)
    expect(actionBodies).toHaveLength(0)
    await page.screenshot({ path: artifactPath('first-run-optional-source-before.png'), fullPage: true })

    const consentNext = dialog.getByRole('button', { name: 'Next' })
    await focusAndCheckControl(page, dialog, consentNext)
    await page.keyboard.press('Enter')
    await expect(dialog.getByText('Sources will be added after initialization when Finish setup is pressed.')).toBeVisible()
    const sourceKind = dialog.getByRole('group', { name: 'Source kind' })
    const directoryRadio = sourceKind.getByRole('radio', { name: 'Directory registry' })
    await focusAndCheckControl(page, dialog, directoryRadio)
    await page.keyboard.press('Space')
    await expect(directoryRadio).toBeChecked()
    const location = dialog.getByLabel('Source location')
    const longDraft = join(home, `pending-${'source-'.repeat(12)}`)
    await focusAndCheckControl(page, dialog, location)
    await location.pressSequentially(longDraft)
    const addPending = dialog.getByRole('button', { name: 'Add source' })
    await focusAndCheckControl(page, dialog, addPending)
    await page.keyboard.press('Enter')
    const pending = dialog.getByLabel('Pending sources')
    await expect(pending).toContainText(longDraft)
    const pendingLocation = pending.getByText(longDraft, { exact: true })
    const textRects = await pendingLocation.evaluate((element) => {
      const range = document.createRange()
      range.selectNodeContents(element)
      return Array.from(range.getClientRects()).map((rect) => ({ left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom }))
    })
    expect(new Set(textRects.map((rect) => Math.round(rect.top))).size).toBeGreaterThan(1)
    const dialogBox = await dialog.boundingBox()
    const viewport = page.viewportSize()
    expect(dialogBox).not.toBeNull()
    expect(viewport).toEqual({ width: 1280, height: 720 })
    expect(textRects.length).toBeGreaterThan(0)
    expect(textRects.every((rect) => rect.left >= dialogBox!.x && rect.right <= dialogBox!.x + dialogBox!.width && rect.left >= 0 && rect.right <= viewport!.width)).toBe(true)
    expect(actionBodies).toHaveLength(0)
    await page.screenshot({ path: artifactPath('first-run-optional-source-after.png'), fullPage: true })

    const removePending = dialog.getByRole('button', { name: `Remove source ${longDraft}` })
    await focusAndCheckControl(page, dialog, removePending)
    await page.keyboard.press('Enter')
    await expect(dialog.getByLabel('Pending sources')).toHaveCount(0)
    expect(actionBodies).toHaveLength(0)
    const cancel = dialog.getByRole('button', { name: 'Cancel', exact: true })
    await focusAndCheckControl(page, dialog, cancel)
    await page.keyboard.press('Enter')
    await expect(dialog).not.toBeVisible()
    expect(existsSync(userConfigPath)).toBe(false)
    expect(existsSync(userLockPath)).toBe(false)
    expect(existsSync(join(home, '.factory', 'skills'))).toBe(false)
    expect(existsSync(join(home, '.devin', 'skills'))).toBe(false)
    expect((await readFile(repoConfigPath)).equals(repoConfigBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    expect(xdgEntries()).toEqual(['<absent>', '<absent>', '<absent>'])
    expect(actionBodies).toHaveLength(0)

    await focusAndCheckControl(page, page.locator('body'), setupButton)
    await page.keyboard.press('Enter')
    await expect(dialog).toBeVisible()
    await expect(factory).toBeChecked()
    await expect(devin).not.toBeChecked()
    await focusAndCheckControl(page, dialog, dialog.getByRole('button', { name: 'Next' }))
    await page.keyboard.press('Enter')
    await expect(yes).not.toBeChecked()
    await expect(no).not.toBeChecked()
    await focusAndCheckControl(page, dialog, yes)
    await page.keyboard.press('Space')
    await focusAndCheckControl(page, dialog, dialog.getByRole('button', { name: 'Next' }))
    await page.keyboard.press('Enter')
    await expect(dialog.getByLabel('Pending sources')).toHaveCount(0)
    await expect(directoryRadio).toBeChecked()
    await expect(location).toHaveValue('')
    const finish = dialog.getByRole('button', { name: 'Finish setup' })
    await focusAndCheckControl(page, dialog, finish)
    expect(actionBodies).toHaveLength(0)
    await page.keyboard.press('Enter')
    await expect(page.getByText('mutation complete')).toBeVisible({ timeout: 60_000 })
    await expect(dialog).not.toBeVisible()

    const written = await readFile(userConfigPath, 'utf8')
    expect(written).toBe(
      'apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\ntelemetry:\n  level: basic\n',
    )
    expect(actionBodies).toHaveLength(1)
    const initializeBody = JSON.parse(actionBodies[0]) as Record<string, unknown>
    expect(initializeBody).toEqual({ action: 'initialize', targets: ['factory'], consent: true })
    expect('sources' in initializeBody).toBe(false)
    expect(existsSync(join(home, '.factory', 'skills'))).toBe(false)
    expect(existsSync(join(home, '.devin'))).toBe(false)
    expect((await readFile(repoConfigPath)).equals(repoConfigBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    expect(xdgEntries()).toEqual(['<absent>', '<absent>', '<absent>'])
  } finally {
    await harness.stop()
  }
})

// spec: gui-first-run/optional-source-step-uses-the-configuration-form
test('built GUI initializes a queued directory source only after Finish setup', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }
  await page.setViewportSize({ width: 1280, height: 720 })
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'first-run' })
  const { bootstrap } = harness
  const home = bootstrap.home!
  const sourceDir = bootstrap.addSourceDir!
  const userConfigPath = join(home, '.agents', 'config.yaml')
  const userLockPath = join(home, '.agents', 'lock.yaml')
  const artifactPath = (name: string) => firstRunArtifactPath(testInfo, name)

  try {
    expect(existsSync(userConfigPath)).toBe(false)
    expect(existsSync(userLockPath)).toBe(false)
    const actionBodies = observeConfigurationActions(page)
    await launchBuiltUserApp(page, bootstrap)
    const dialog = page.getByRole('dialog', { name: 'Set up Agent Marketplace' })
    await expect(dialog).toBeVisible({ timeout: 60_000 })

    const factory = dialog.getByRole('checkbox', { name: 'Target factory' })
    await expect(factory).toBeChecked()
    await focusAndCheckControl(page, dialog, factory)
    await page.keyboard.press('Space')
    await expect(factory).not.toBeChecked()
    await page.keyboard.press('Space')
    await expect(factory).toBeChecked()
    await focusAndCheckControl(page, dialog, dialog.getByRole('button', { name: 'Next' }))
    await page.keyboard.press('Enter')

    const no = dialog.getByRole('radio', { name: /no/i })
    await expect(no).not.toBeChecked()
    await focusAndCheckControl(page, dialog, no)
    await page.keyboard.press('Space')
    await expect(no).toBeChecked()
    await focusAndCheckControl(page, dialog, dialog.getByRole('button', { name: 'Next' }))
    await page.keyboard.press('Enter')

    const sourceKind = dialog.getByRole('group', { name: 'Source kind' })
    const directoryRadio = sourceKind.getByRole('radio', { name: 'Directory registry' })
    await focusAndCheckControl(page, dialog, directoryRadio)
    await page.keyboard.press('Space')
    const location = dialog.getByLabel('Source location')
    await focusAndCheckControl(page, dialog, location)
    await location.pressSequentially(sourceDir)
    const addButton = dialog.getByRole('button', { name: 'Add source' })
    await focusAndCheckControl(page, dialog, addButton)
    expect(actionBodies).toHaveLength(0)
    await page.keyboard.press('Enter')
    const pending = dialog.getByLabel('Pending sources')
    await expect(pending).toContainText(sourceDir)
    expect(actionBodies).toHaveLength(0)
    const finish = dialog.getByRole('button', { name: 'Finish setup' })
    await focusAndCheckControl(page, dialog, finish)
    await page.screenshot({ path: artifactPath('first-run-valid-source-queued.png'), fullPage: true })
    await page.keyboard.press('Enter')

    await expect(page.getByLabel('Notifications').getByText('mutation complete')).toBeVisible({ timeout: 60_000 })
    await expect(dialog).not.toBeVisible()
    const expectedRoot = `apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\nsources:\n  optional-directory:\n    directory: ${sourceDir}\n`
    await expect.poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 }).toBe(expectedRoot)
    expect(actionBodies).toHaveLength(1)
    expect(JSON.parse(actionBodies[0])).toEqual({
      action: 'initialize',
      targets: ['factory'],
      consent: false,
      sources: [{ kind: 'directory', location: sourceDir }],
    })
    expect(existsSync(userLockPath)).toBe(false)
    expect(existsSync(join(home, '.factory', 'skills'))).toBe(false)
    expect(existsSync(join(home, '.agents', 'skills'))).toBe(false)

    const sourcesButton = page.getByRole('button', { name: 'Sources' })
    await focusAndCheckControl(page, page.locator('body'), sourcesButton)
    await page.keyboard.press('Enter')
    const sourcesDialog = page.getByRole('dialog', { name: 'Sources' })
    await expect(sourcesDialog).toBeVisible()
    await expect(sourcesDialog.getByLabel('User sources')).toContainText(sourceDir)
    await page.screenshot({ path: artifactPath('first-run-valid-source-registered.png'), fullPage: true })
  } finally {
    await harness.stop()
  }
})

// spec: gui-first-run/optional-source-failure-keeps-initialized-configuration
test('built GUI keeps initialized sources on optional failure and retries from Sources settings', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }
  await page.setViewportSize({ width: 1280, height: 720 })
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'first-run' })
  const { bootstrap } = harness
  const home = bootstrap.home!
  const repo = bootstrap.repo!
  const sourceDir = bootstrap.addSourceDir!
  const secondDirectory = join(home, 'never-added-directory')
  mkdirSync(join(secondDirectory, 'resource'), { recursive: true })
  const endpoint = bootstrap.marketplaceEndpoint!
  const userConfigPath = join(home, '.agents', 'config.yaml')
  const userLockPath = join(home, '.agents', 'lock.yaml')
  const repoConfigPath = join(repo, '.agents', 'config.yaml')
  const repoLocalPath = join(repo, '.agents', 'config.local.yaml')
  const repoConfigBefore = await readFile(repoConfigPath)
  const repoLocalBefore = await readFile(repoLocalPath)
  const xdgDirs = [bootstrap.xdgConfig!, bootstrap.xdgState!, bootstrap.xdgCache!]
  const xdgEntries = () =>
    xdgDirs.map((dir) => (existsSync(dir) ? readdirSync(dir).sort().join(',') : '<absent>'))
  const artifactPath = (name: string) => firstRunArtifactPath(testInfo, name)

  try {
    expect(existsSync(userConfigPath)).toBe(false)
    expect(existsSync(userLockPath)).toBe(false)
    const actionBodies = observeConfigurationActions(page)
    await launchBuiltUserApp(page, bootstrap)
    const dialog = page.getByRole('dialog', { name: 'Set up Agent Marketplace' })
    await expect(dialog).toBeVisible({ timeout: 60_000 })

    const factory = dialog.getByRole('checkbox', { name: 'Target factory' })
    await expect(factory).toBeChecked()
    await focusAndCheckControl(page, dialog, factory)
    await page.keyboard.press('Space')
    await page.keyboard.press('Space')
    await expect(factory).toBeChecked()
    await focusAndCheckControl(page, dialog, dialog.getByRole('button', { name: 'Next' }))
    await page.keyboard.press('Enter')
    const no = dialog.getByRole('radio', { name: /no/i })
    await focusAndCheckControl(page, dialog, no)
    await page.keyboard.press('Space')
    await expect(no).toBeChecked()
    await focusAndCheckControl(page, dialog, dialog.getByRole('button', { name: 'Next' }))
    await page.keyboard.press('Enter')

    const sourceKind = dialog.getByRole('group', { name: 'Source kind' })
    const directoryRadio = sourceKind.getByRole('radio', { name: 'Directory registry' })
    const marketplaceRadio = sourceKind.getByRole('radio', { name: 'Marketplace endpoint' })
    await focusAndCheckControl(page, dialog, directoryRadio)
    await page.keyboard.press('Space')
    const location = dialog.getByLabel('Source location')
    const addButton = dialog.getByRole('button', { name: 'Add source' })
    await focusAndCheckControl(page, dialog, location)
    await location.pressSequentially(sourceDir)
    await focusAndCheckControl(page, dialog, addButton)
    await page.keyboard.press('Enter')
    await expect(dialog.getByLabel('Pending sources')).toContainText(sourceDir)

    await focusAndCheckControl(page, dialog, marketplaceRadio)
    await page.keyboard.press('Space')
    await focusAndCheckControl(page, dialog, location)
    await location.pressSequentially(endpoint)
    await focusAndCheckControl(page, dialog, addButton)
    await page.keyboard.press('Enter')
    await expect(dialog.getByLabel('Pending sources')).toContainText(endpoint)

    await focusAndCheckControl(page, dialog, directoryRadio)
    await page.keyboard.press('Space')
    await focusAndCheckControl(page, dialog, location)
    await location.pressSequentially(secondDirectory)
    await focusAndCheckControl(page, dialog, addButton)
    await page.keyboard.press('Enter')
    const pending = dialog.getByLabel('Pending sources')
    await expect(pending).toContainText(sourceDir)
    await expect(pending).toContainText(endpoint)
    await expect(pending).toContainText(secondDirectory)
    expect(actionBodies).toHaveLength(0)
    const finish = dialog.getByRole('button', { name: 'Finish setup' })
    await focusAndCheckControl(page, dialog, finish)
    await page.screenshot({ path: artifactPath('first-run-optional-sources-queued.png'), fullPage: true })
    await page.keyboard.press('Enter')

    await expect(page.getByLabel('Notifications').getByText('mutation failed')).toBeVisible({ timeout: 60_000 })
    await expect(page.getByLabel('Notifications').getByText('mutation complete')).toHaveCount(0)
    await expect(dialog).not.toBeVisible()
    expect(actionBodies).toHaveLength(1)
    const initializeBody = JSON.parse(actionBodies[0]) as { action: string; sources: { kind: string; location: string }[] }
    expect(initializeBody.action).toBe('initialize')
    expect(initializeBody.sources).toEqual([
      { kind: 'directory', location: sourceDir },
      { kind: 'marketplace', location: endpoint },
      { kind: 'directory', location: secondDirectory },
    ])

    const expectedFailureRoot = `apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\nsources:\n  optional-directory:\n    directory: ${sourceDir}\n`
    await expect.poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 }).toBe(expectedFailureRoot)
    expect(existsSync(userLockPath)).toBe(false)
    expect(existsSync(join(home, '.factory', 'skills'))).toBe(false)
    expect(existsSync(join(home, '.agents', 'skills'))).toBe(false)
    expect((await readFile(repoConfigPath)).equals(repoConfigBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    expect(xdgEntries()).toEqual(['<absent>', 'agent-marketplace', '<absent>'])

    const configuration = await page.evaluate(async ({ url, credential }: { url: string; credential: string }) => {
      const response = await fetch(`${url}/api/configuration`, {
        method: 'POST',
        headers: { 'X-Agent-Marketplace-Token': credential },
      })
      return await response.json() as Record<string, unknown>
    }, { url: bootstrap.url, credential: bootstrap.credential })
    expect(configuration.rootPresent).toBe(true)
    expect(configuration.firstRun).toBe(false)
    expect(configuration.editable).toBe(true)
    expect(configuration.userSources).toMatchObject([
      expect.objectContaining({ kind: 'directory', location: sourceDir }),
    ])

    const sourcesButton = page.getByRole('button', { name: 'Sources' })
    await focusAndCheckControl(page, page.locator('body'), sourcesButton)
    await page.keyboard.press('Enter')
    const sourcesDialog = page.getByRole('dialog', { name: 'Sources' })
    await expect(sourcesDialog).toBeVisible()
    const userSources = sourcesDialog.getByLabel('User sources')
    await expect(userSources).toContainText(sourceDir)
    await expect(userSources).not.toContainText(endpoint)
    await expect(userSources).not.toContainText(secondDirectory)
    await expect(sourcesDialog).toContainText('add optional source:')
    await page.screenshot({ path: artifactPath('first-run-optional-source-failure.png'), fullPage: true })

    const retryKind = sourcesDialog.getByRole('group', { name: 'Source kind' })
    const retryMarketplace = retryKind.getByRole('radio', { name: 'Marketplace endpoint' })
    await focusAndCheckControl(page, sourcesDialog, retryMarketplace)
    await page.keyboard.press('Space')
    const retryLocation = sourcesDialog.getByLabel('Source location')
    await focusAndCheckControl(page, sourcesDialog, retryLocation)
    await retryLocation.pressSequentially(endpoint)
    const retryAdd = sourcesDialog.getByRole('button', { name: 'Add source' })
    await focusAndCheckControl(page, sourcesDialog, retryAdd)
    await page.keyboard.press('Enter')
    await expect(userSources).toContainText(endpoint, { timeout: 60_000 })
    await expect(page.getByLabel('Notifications').getByText('mutation complete').last()).toBeVisible({ timeout: 60_000 })
    expect(actionBodies).toHaveLength(2)
    expect(JSON.parse(actionBodies[1])).toEqual({
      action: 'add-source',
      source: { kind: 'marketplace', location: endpoint },
    })
    await expect(userSources).toContainText(sourceDir)
    await expect(userSources).not.toContainText(secondDirectory)
    await expect.poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 }).toContain(endpoint)
    const lockBytes = await readFile(userLockPath, 'utf8')
    expect(lockBytes).toContain(endpoint)
    expect(lockBytes).not.toContain('resources:')
    expect(existsSync(join(home, '.factory', 'skills'))).toBe(false)
    await page.screenshot({ path: artifactPath('first-run-optional-source-retried.png'), fullPage: true })
  } finally {
    await harness.stop()
  }
})
