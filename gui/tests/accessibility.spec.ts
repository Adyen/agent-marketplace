import { expect, test, type Locator, type Page, type TestInfo } from '@playwright/test'
import { existsSync, readdirSync } from 'node:fs'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { startHarness } from './harness'
import { distIndexExists, launchBuiltUserApp } from './app'
import { tabTo } from './keyboard'

function evidencePath(testInfo: TestInfo, name: string): string {
  const directory = process.env.AM_WORK32_KEYBOARD_EVIDENCE_DIR
  return directory == null || directory === '' ? testInfo.outputPath(name) : join(directory, name)
}

async function pressKey(page: Page, key: string, label: string, actions: string[]): Promise<void> {
  actions.push(`${key}: ${label}`)
  await page.keyboard.press(key)
}

async function typeKeyboard(page: Page, value: string, label: string, actions: string[]): Promise<void> {
  actions.push(`type: ${label}`)
  await page.keyboard.type(value)
}

async function assertModalTrap(page: Page, dialog: Locator, name: string, actions: string[]): Promise<void> {
  const controls: Locator[] = []
  for (const button of await dialog.getByRole('button').all()) {
    if (await button.isVisible() && await button.isEnabled()) {
      controls.push(button)
    }
  }
  expect(controls.length, `${name} should expose multiple visible keyboard buttons`).toBeGreaterThan(1)
  const first = controls[0]
  const last = controls[controls.length - 1]
  await tabTo(page, last, `${name} last button`, actions, dialog)
  await tabTo(page, first, `${name} wrapped first button`, actions, dialog)
  await tabTo(page, last, `${name} reverse-wrapped last button`, actions, dialog, 'Shift+Tab')
}

function armPost(page: Page, pathname: string) {
  return page.waitForResponse((response) =>
    response.request().method() === 'POST' && new URL(response.url()).pathname === pathname,
  )
}

async function completeResponse(responsePromise: ReturnType<typeof armPost>, status: number) {
  const response = await responsePromise
  expect(await response.finished()).toBeNull()
  expect(response.status()).toBe(status)
  return response
}

async function waitForOperationCompletion(page: Page, action: 'sync' | 'update'): Promise<void> {
  await expect(page.getByLabel('Notifications').getByText(new RegExp(`^${action} complete( with \\d+ warning\\(s\\))?$`))).toBeVisible({ timeout: 60_000 })
  await expect(page.locator('.gui-toast--progress')).toHaveCount(0)
}

async function saveActions(testInfo: TestInfo, name: string, actions: string[]): Promise<void> {
  const path = evidencePath(testInfo, name)
  await mkdir(dirname(path), { recursive: true })
  await writeFile(path, `${JSON.stringify({ actions }, null, 2)}\n`)
}

function configurationActionCounter(page: Page): () => number {
  let count = 0
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/configuration/actions') {
      count++
    }
  })
  return () => count
}

function xdgEntries(bootstrap: { xdgConfig?: string; xdgState?: string; xdgCache?: string }): string[] {
  return [bootstrap.xdgConfig, bootstrap.xdgState, bootstrap.xdgCache].map((directory) =>
    directory == null || !existsSync(directory) ? '<absent>' : readdirSync(directory).sort().join(','),
  )
}

async function expectOptionalFileUnchanged(path: string, before: Buffer | null): Promise<void> {
  if (before == null) {
    expect(existsSync(path)).toBe(false)
    return
  }
  expect((await readFile(path)).equals(before)).toBe(true)
}

// spec: gui-shell/all-actions-are-keyboard-reachable-and-named
test('built GUI traverses shell, resource, sources, settings, and quit controls by keyboard', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the accessibility journey')
  }

  const actions: string[] = []
  const harness = await startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'sources',
    AM_LOCALAPI_RELEASE_VERSION: 'dev',
  })
  const { bootstrap } = harness
  const home = bootstrap.home!
  const repo = bootstrap.repo!
  const sourceDir = bootstrap.addSourceDir!
  const userConfigPath = join(home, '.agents', 'config.yaml')
  const fragmentPath = join(home, '.agents', 'fragment.yaml')
  const personalLocalPath = join(home, '.agents', 'config.local.yaml')
  const organizationPath = bootstrap.orgConfig!
  const repoConfigPath = join(repo, '.agents', 'config.yaml')
  const repoLocalPath = join(repo, '.agents', 'config.local.yaml')
  const configurationActionCount = configurationActionCounter(page)

  try {
    const fragmentBefore = await readFile(fragmentPath)
    const organizationBefore = await readFile(organizationPath)
    const repoBefore = await readFile(repoConfigPath)
    const repoLocalBefore = await readFile(repoLocalPath)
    const personalLocalBefore = existsSync(personalLocalPath) ? await readFile(personalLocalPath) : null

    await page.setViewportSize({ width: 1280, height: 800 })
    await launchBuiltUserApp(page, bootstrap)
    await expect(page.getByText('solo', { exact: true }).first()).toBeVisible({ timeout: 60_000 })
    await page.screenshot({ path: evidencePath(testInfo, 'sources-start.png'), fullPage: true, animations: 'disabled' })

    const refresh = page.getByRole('button', { name: 'Refresh', exact: true })
    const sync = page.getByRole('button', { name: 'Sync', exact: true })
    const update = page.getByRole('button', { name: 'Update', exact: true })
    const sourcesButton = page.getByRole('button', { name: 'Sources', exact: true })
    const settingsButton = page.getByRole('button', { name: 'Settings', exact: true })
    const quitButton = page.getByRole('button', { name: 'Quit', exact: true })
    await expect(refresh).toHaveAccessibleName('Refresh')
    await expect(sync).toHaveAccessibleName('Sync')
    await expect(update).toHaveAccessibleName('Update')
    await expect(sourcesButton).toHaveAccessibleName('Sources')
    await expect(settingsButton).toHaveAccessibleName('Settings')
    await expect(quitButton).toHaveAccessibleName('Quit')

    await tabTo(page, refresh, 'Refresh button', actions)
    const refreshResponse = armPost(page, '/api/snapshot')
    await pressKey(page, 'Enter', 'Refresh', actions)
    await completeResponse(refreshResponse, 200)
    await expect(page.getByText('solo', { exact: true }).first()).toBeVisible()
    await expect(sync).toBeEnabled({ timeout: 60_000 })

    await tabTo(page, sync, 'Sync button', actions)
    const syncResponse = armPost(page, '/api/actions')
    await pressKey(page, 'Enter', 'Sync', actions)
    await completeResponse(syncResponse, 202)
    await waitForOperationCompletion(page, 'sync')
    actions.push('Sync reached authoritative final state')
    await expect(update).toBeEnabled({ timeout: 60_000 })

    await tabTo(page, update, 'Update button', actions)
    const updateResponse = armPost(page, '/api/actions')
    await pressKey(page, 'Enter', 'Update', actions)
    await completeResponse(updateResponse, 202)
    await waitForOperationCompletion(page, 'update')
    actions.push('Update reached authoritative final state')

    const resource = page.getByRole('option', { name: /solo/ })
    await tabTo(page, resource, 'Resource option solo', actions)
    await pressKey(page, 'Enter', 'Inspect resource', actions)
    const detail = page.getByRole('complementary', { name: 'Detail' })
    await expect(detail).toContainText('solo')
    await expect(detail).toContainText('Solo skill')

    await tabTo(page, sourcesButton, 'Sources button', actions)
    await pressKey(page, 'Enter', 'Open Sources', actions)
    const sourcesDialog = page.getByRole('dialog', { name: 'Settings' })
    await expect(sourcesDialog).toBeVisible()
    const userSources = sourcesDialog.getByLabel('User sources')
    await expect(userSources).toBeVisible({ timeout: 60_000 })
    const sourceKind = sourcesDialog.getByRole('group', { name: 'Source kind' })
    const gitRadio = sourceKind.getByRole('radio', { name: 'Git registry' })
    const directoryRadio = sourceKind.getByRole('radio', { name: 'Directory registry' })
    const sourceLocation = sourcesDialog.getByRole('textbox', { name: 'Source location' })
    await expect(sourceLocation).toBeEnabled({ timeout: 60_000 })
    await assertModalTrap(page, sourcesDialog, 'Sources', actions)
    await expect(directoryRadio).toBeChecked()
    await tabTo(page, directoryRadio, 'Directory registry radio', actions, sourcesDialog)
    await pressKey(page, 'ArrowDown', 'Select Git registry option', actions)
    await expect(gitRadio).toBeChecked()
    const sourceRef = sourcesDialog.getByRole('textbox', { name: 'Source ref' })
    const sourceSubpath = sourcesDialog.getByRole('textbox', { name: 'Source subpath' })
    const addSource = sourcesDialog.getByRole('button', { name: 'Add source', exact: true })
    await expect(sourceRef).toHaveAccessibleName('Source ref')
    await expect(sourceSubpath).toHaveAccessibleName('Source subpath')
    await expect(sourceRef).toBeEnabled()
    await expect(sourceSubpath).toBeEnabled()
    const rootBeforeSourceDraft = await readFile(userConfigPath)
    const actionsBeforeSourceDraft = configurationActionCount()
    await tabTo(page, sourceRef, 'Source ref textbox', actions, sourcesDialog)
    await typeKeyboard(page, 'work32-draft-ref', 'safe Git source ref draft', actions)
    await expect(sourceRef).toHaveValue('work32-draft-ref')
    await tabTo(page, sourceSubpath, 'Source subpath textbox', actions, sourcesDialog)
    await typeKeyboard(page, 'work32/draft', 'safe Git source subpath draft', actions)
    await expect(sourceSubpath).toHaveValue('work32/draft')
    await expect(addSource).toBeDisabled()
    expect(configurationActionCount()).toBe(actionsBeforeSourceDraft)
    expect((await readFile(userConfigPath)).equals(rootBeforeSourceDraft)).toBe(true)

    await tabTo(page, gitRadio, 'Git registry radio before Marketplace', actions, sourcesDialog)
    const marketplaceRadio = sourceKind.getByRole('radio', { name: 'Marketplace endpoint' })
    await pressKey(page, 'ArrowDown', 'Select Marketplace endpoint option', actions)
    await expect(marketplaceRadio).toHaveAccessibleName('Marketplace endpoint')
    await expect(marketplaceRadio).toBeChecked()
    await pressKey(page, 'ArrowUp', 'Return to Git registry option', actions)
    await expect(gitRadio).toBeChecked()
    await pressKey(page, 'ArrowUp', 'Return to Directory registry option', actions)
    await expect(directoryRadio).toBeChecked()
    expect(configurationActionCount()).toBe(actionsBeforeSourceDraft)
    expect((await readFile(userConfigPath)).equals(rootBeforeSourceDraft)).toBe(true)

    await expect(sourceLocation).toHaveAccessibleName('Source location')
    await tabTo(page, sourceLocation, 'Source location textbox', actions, sourcesDialog)
    await typeKeyboard(page, sourceDir, 'private directory source location', actions)
    await expect(addSource).toBeEnabled()
    const addSourceResponse = armPost(page, '/api/configuration/actions')
    await tabTo(page, addSource, 'Add source button', actions, sourcesDialog)
    await pressKey(page, 'Enter', 'Add user source', actions)
    await completeResponse(addSourceResponse, 202)
    await expect(userSources).toContainText(sourceDir, { timeout: 60_000 })
    await expect(page.getByLabel('Notifications').getByText('mutation complete').last()).toBeVisible({ timeout: 60_000 })
    await expect(sourcesDialog.getByText('Applying changes…')).toHaveCount(0)
    await expect.poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 }).toContain(sourceDir)
    const addedRoot = await readFile(userConfigPath)
    expect((await readFile(fragmentPath)).equals(fragmentBefore)).toBe(true)
    expect((await readFile(organizationPath)).equals(organizationBefore)).toBe(true)
    expect((await readFile(repoConfigPath)).equals(repoBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    await expectOptionalFileUnchanged(personalLocalPath, personalLocalBefore)

    const removeSource = userSources.getByRole('button', { name: `Remove source ${sourceDir}` })
    await tabTo(page, removeSource, 'Remove source action', actions, sourcesDialog)
    await pressKey(page, 'Enter', 'Request source removal', actions)
    const cancelRemove = sourcesDialog.getByRole('button', { name: 'Cancel remove', exact: true })
    await expect(cancelRemove).toBeVisible()
    await tabTo(page, cancelRemove, 'Cancel source removal button', actions, sourcesDialog)
    await pressKey(page, 'Enter', 'Cancel source removal', actions)
    expect((await readFile(userConfigPath)).equals(addedRoot)).toBe(true)
    await expect(userSources).toContainText(sourceDir)

    await tabTo(page, removeSource, 'Remove source action', actions, sourcesDialog)
    await pressKey(page, 'Enter', 'Request source removal again', actions)
    const confirmRemove = sourcesDialog.getByRole('button', { name: 'Confirm remove', exact: true })
    await expect(confirmRemove).toBeVisible()
    const removeSourceResponse = armPost(page, '/api/configuration/actions')
    await tabTo(page, confirmRemove, 'Confirm remove button', actions, sourcesDialog)
    await pressKey(page, 'Enter', 'Confirm source removal', actions)
    await completeResponse(removeSourceResponse, 202)
    await expect(userSources.getByText(sourceDir)).toHaveCount(0, { timeout: 60_000 })
    await expect(page.getByLabel('Notifications').getByText('mutation complete').last()).toBeVisible({ timeout: 60_000 })
    await expect(sourcesDialog.getByText('Applying changes…')).toHaveCount(0)
    const removedRoot = await readFile(userConfigPath, 'utf8')
    expect(removedRoot).toContain('include: fragment.yaml')
    expect(removedRoot).toContain(join(home, 'keep-source'))
    expect(removedRoot).not.toContain(sourceDir)
    expect((await readFile(fragmentPath)).equals(fragmentBefore)).toBe(true)
    expect((await readFile(organizationPath)).equals(organizationBefore)).toBe(true)
    expect((await readFile(repoConfigPath)).equals(repoBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)

    await page.keyboard.press('Escape')
    actions.push('Escape: close Sources')
    await expect(sourcesDialog).not.toBeVisible()
    await expect(sourcesButton).toBeFocused()

    await tabTo(page, settingsButton, 'Settings button', actions)
    await pressKey(page, 'Enter', 'Open Settings', actions)
    const settingsDialog = page.getByRole('dialog', { name: 'Settings' })
    await expect(settingsDialog).toBeVisible()
    await settingsDialog.getByRole('tab', { name: 'Harness targets' }).click()
    const devin = settingsDialog.getByRole('checkbox', { name: 'Target devin' })
    const claude = settingsDialog.getByRole('checkbox', { name: 'Target claude' })
    await expect(devin).toBeEnabled({ timeout: 60_000 })
    await assertModalTrap(page, settingsDialog, 'Settings', actions)
    await expect(devin).toBeChecked()
    await expect(claude).not.toBeChecked()
    const rootBeforeSettings = await readFile(userConfigPath)
    const actionsBeforeCancel = configurationActionCount()
    await tabTo(page, claude, 'Target claude checkbox', actions, settingsDialog)
    await pressKey(page, 'Space', 'Add claude target draft', actions)
    await expect(claude).toBeChecked()
    const cancelSettings = settingsDialog.getByRole('button', { name: 'Close dialog', exact: true })
    await tabTo(page, cancelSettings, 'Cancel settings button', actions, settingsDialog)
    await pressKey(page, 'Enter', 'Cancel settings draft', actions)
    await expect(settingsDialog).not.toBeVisible()
    await expect(settingsButton).toBeFocused()
    expect((await readFile(userConfigPath)).equals(rootBeforeSettings)).toBe(true)
    expect(configurationActionCount()).toBe(actionsBeforeCancel)

    await pressKey(page, 'Enter', 'Reopen Settings', actions)
    await expect(settingsDialog).toBeVisible()
    await settingsDialog.getByRole('tab', { name: 'Harness targets' }).click()
    await expect(devin).toBeEnabled({ timeout: 60_000 })
    await assertModalTrap(page, settingsDialog, 'Settings', actions)
    await expect(devin).toBeChecked()
    await expect(claude).not.toBeChecked()
    await tabTo(page, claude, 'Target claude checkbox', actions, settingsDialog)
    await pressKey(page, 'Space', 'Select claude target', actions)
    await expect(claude).toBeChecked()
    const saveTargets = settingsDialog.getByRole('button', { name: 'Save targets', exact: true })
    await expect(saveTargets).toBeEnabled()
    const actionsBeforeSave = configurationActionCount()
    const saveTargetsResponse = armPost(page, '/api/configuration/actions')
    await tabTo(page, saveTargets, 'Save targets button', actions, settingsDialog)
    await pressKey(page, 'Enter', 'Save targets', actions)
    const savedTargetsResponse = await completeResponse(saveTargetsResponse, 202)
    const savedTargetsRequest = savedTargetsResponse.request().postDataJSON() as { action: string; targets: string[] }
    expect(savedTargetsRequest.action).toBe('save-targets')
    expect(savedTargetsRequest.targets).toEqual(expect.arrayContaining(['devin', 'claude']))
    expect(savedTargetsRequest.targets.length).toBeGreaterThan(0)
    expect(configurationActionCount()).toBe(actionsBeforeSave + 1)
    await expect(settingsDialog).toBeVisible()
    await expect(page.getByLabel('Notifications').getByText('targets complete').last()).toBeVisible({ timeout: 60_000 })
    await expect.poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 }).toContain('claude')
    const savedRoot = await readFile(userConfigPath, 'utf8')
    expect(savedRoot).toContain('harnesses:')
    expect(savedRoot).toContain('devin')
    expect(savedRoot).toContain('claude')
    expect(savedRoot).toContain(join(home, 'keep-source'))
    expect((await readFile(fragmentPath)).equals(fragmentBefore)).toBe(true)
    expect((await readFile(organizationPath)).equals(organizationBefore)).toBe(true)
    expect((await readFile(repoConfigPath)).equals(repoBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    await expectOptionalFileUnchanged(personalLocalPath, personalLocalBefore)

    const closeSettings = settingsDialog.getByRole('button', { name: 'Close dialog', exact: true })
    await closeSettings.focus()
    actions.push('Focused the Close Settings button after Save; the window stays open after a save')
    await pressKey(page, 'Enter', 'Close Settings', actions)
    await expect(settingsDialog).not.toBeVisible()
    await expect(settingsButton).toBeFocused()

    await tabTo(page, quitButton, 'Quit button', actions)
    await pressKey(page, 'Enter', 'Open Quit', actions)
    const quitDialog = page.getByRole('dialog', { name: 'Quit Agent Marketplace' })
    await expect(quitDialog).toBeVisible()
    await assertModalTrap(page, quitDialog, 'Quit', actions)
    await page.screenshot({ path: evidencePath(testInfo, 'quit-dialog.png'), fullPage: true, animations: 'disabled' })
    const cancelQuit = quitDialog.getByRole('button', { name: 'Cancel quit', exact: true })
    await expect(cancelQuit).toHaveAccessibleName('Cancel quit')
    await tabTo(page, cancelQuit, 'Cancel quit button', actions, quitDialog)
    await pressKey(page, 'Enter', 'Cancel Quit', actions)
    await expect(quitDialog).not.toBeVisible()
    await expect(quitButton).toBeFocused()

    await page.screenshot({ path: evidencePath(testInfo, 'sources-settings-quit.png'), fullPage: true, animations: 'disabled' })
    actions.push('Sources, settings, and quit flow completed with keyboard only')
  } finally {
    await harness.stop()
    await saveActions(testInfo, 'sources-settings.actions.json', actions)
  }
})

// spec: gui-shell/all-actions-are-keyboard-reachable-and-named
test('built GUI cancels and completes first-run setup using keyboard only', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the accessibility journey')
  }

  const actions: string[] = []
  const harness = await startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'first-run',
    AM_LOCALAPI_RELEASE_VERSION: 'dev',
  })
  const { bootstrap } = harness
  const home = bootstrap.home!
  const repo = bootstrap.repo!
  const sourceDir = bootstrap.addSourceDir!
  const userConfigPath = join(home, '.agents', 'config.yaml')
  const userLockPath = join(home, '.agents', 'lock.yaml')
  const personalLocalPath = join(home, '.agents', 'config.local.yaml')
  const repoConfigPath = join(repo, '.agents', 'config.yaml')
  const repoLocalPath = join(repo, '.agents', 'config.local.yaml')
  const configurationActionCount = configurationActionCounter(page)

  try {
    expect(bootstrap.orgConfig).toBeUndefined()
    expect(existsSync(userConfigPath)).toBe(false)
    expect(existsSync(userLockPath)).toBe(false)
    const repoBefore = await readFile(repoConfigPath)
    const repoLocalBefore = await readFile(repoLocalPath)
    const personalLocalBefore = existsSync(personalLocalPath) ? await readFile(personalLocalPath) : null
    const organizationBefore = bootstrap.orgConfig == null ? null : await readFile(bootstrap.orgConfig)
    const xdgBefore = xdgEntries(bootstrap)

    await page.setViewportSize({ width: 1280, height: 800 })
    await launchBuiltUserApp(page, bootstrap)
    const dialog = page.getByRole('dialog', { name: 'Set up Agent Marketplace' })
    await expect(dialog).toBeVisible({ timeout: 60_000 })
    await assertModalTrap(page, dialog, 'First-run setup', actions)

    const factory = dialog.getByRole('checkbox', { name: 'Target factory' })
    const devin = dialog.getByRole('checkbox', { name: 'Target devin' })
    await expect(factory).toBeChecked()
    await expect(devin).not.toBeChecked()
    await expect(factory).toHaveAccessibleName('Target factory')
    await expect(devin).toHaveAccessibleName('Target devin')
    await tabTo(page, devin, 'Target devin checkbox', actions, dialog)
    await pressKey(page, 'Space', 'Select devin target', actions)
    await expect(devin).toBeChecked()

    const next = dialog.getByRole('button', { name: 'Next', exact: true })
    await tabTo(page, next, 'Next from targets', actions, dialog)
    await pressKey(page, 'Enter', 'Next to consent', actions)
    const no = dialog.getByRole('radio', { name: 'No, do not send telemetry' })
    const yes = dialog.getByRole('radio', { name: 'Yes, enable basic telemetry' })
    await expect(no).not.toBeChecked()
    await expect(yes).not.toBeChecked()
    await expect(no).toHaveAccessibleName('No, do not send telemetry')
    await tabTo(page, yes, 'Yes telemetry radio in unselected group', actions, dialog)
    await pressKey(page, 'ArrowDown', 'Choose no telemetry', actions)
    await expect(no).toBeChecked()
    await tabTo(page, next, 'Next from consent', actions, dialog)
    await pressKey(page, 'Enter', 'Next to sources', actions)

    const sourceKind = dialog.getByRole('group', { name: 'Source kind' })
    const gitRadio = sourceKind.getByRole('radio', { name: 'Git registry' })
    const directoryRadio = sourceKind.getByRole('radio', { name: 'Directory registry' })
    await expect(directoryRadio).toBeChecked()
    await tabTo(page, directoryRadio, 'Directory registry radio', actions, dialog)
    await pressKey(page, 'ArrowDown', 'Select Git registry option', actions)
    await expect(gitRadio).toBeChecked()
    await pressKey(page, 'ArrowUp', 'Select Directory registry option', actions)
    await expect(directoryRadio).toBeChecked()
    const location = dialog.getByRole('textbox', { name: 'Source location' })
    await tabTo(page, location, 'Source location textbox', actions, dialog)
    await typeKeyboard(page, sourceDir, 'private optional directory source', actions)
    const addSource = dialog.getByRole('button', { name: 'Add source', exact: true })
    await tabTo(page, addSource, 'Add source button', actions, dialog)
    await pressKey(page, 'Enter', 'Queue optional source', actions)
    const pendingSources = dialog.getByLabel('Pending sources')
    await expect(pendingSources).toContainText(sourceDir)
    expect(configurationActionCount()).toBe(0)
    expect(existsSync(userConfigPath)).toBe(false)
    await page.screenshot({ path: evidencePath(testInfo, 'first-run-source-queued.png'), fullPage: true, animations: 'disabled' })

    const back = dialog.getByRole('button', { name: 'Back', exact: true })
    await tabTo(page, back, 'Back from sources', actions, dialog)
    await pressKey(page, 'Enter', 'Back to consent', actions)
    await expect(no).toBeChecked()
    await tabTo(page, back, 'Back from consent', actions, dialog)
    await pressKey(page, 'Enter', 'Back to targets', actions)
    await expect(devin).toBeChecked()
    await tabTo(page, next, 'Next from targets again', actions, dialog)
    await pressKey(page, 'Enter', 'Return to consent', actions)
    await expect(no).toBeChecked()
    await tabTo(page, next, 'Next to queued sources', actions, dialog)
    await pressKey(page, 'Enter', 'Return to sources', actions)
    await expect(pendingSources).toContainText(sourceDir)

    const cancel = dialog.getByRole('button', { name: 'Cancel', exact: true })
    await tabTo(page, cancel, 'Cancel first-run setup', actions, dialog)
    await pressKey(page, 'Enter', 'Cancel before Finish setup', actions)
    await expect(dialog).not.toBeVisible()
    expect(existsSync(userConfigPath)).toBe(false)
    expect(existsSync(userLockPath)).toBe(false)
    expect(configurationActionCount()).toBe(0)
    expect((await readFile(repoConfigPath)).equals(repoBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    await expectOptionalFileUnchanged(personalLocalPath, personalLocalBefore)
    expect(xdgEntries(bootstrap)).toEqual(xdgBefore)

    const setupButton = page.getByRole('button', { name: 'Set up', exact: true })
    await expect(setupButton).toBeVisible()
    await expect(setupButton).toBeFocused()
    await pressKey(page, 'Enter', 'Reopen first-run setup', actions)
    await expect(dialog).toBeVisible()
    await assertModalTrap(page, dialog, 'Reopened first-run setup', actions)
    await expect(factory).toBeChecked()
    await expect(devin).not.toBeChecked()
    await tabTo(page, devin, 'Target devin checkbox after reopen', actions, dialog)
    await pressKey(page, 'Space', 'Select devin target after reopen', actions)
    await tabTo(page, next, 'Next to consent after reopen', actions, dialog)
    await pressKey(page, 'Enter', 'Next to consent after reopen', actions)
    await expect(no).not.toBeChecked()
    await expect(yes).not.toBeChecked()
    await tabTo(page, yes, 'Yes telemetry radio after reopen', actions, dialog)
    await pressKey(page, 'ArrowDown', 'Explicitly choose no telemetry after reopen', actions)
    await expect(no).toBeChecked()
    await tabTo(page, next, 'Next to sources after reopen', actions, dialog)
    await pressKey(page, 'Enter', 'Next to sources after reopen', actions)
    await expect(pendingSources).toHaveCount(0)
    await expect(directoryRadio).toBeChecked()
    await tabTo(page, directoryRadio, 'Directory registry radio after reopen', actions, dialog)
    await pressKey(page, 'ArrowDown', 'Select Git registry after reopen', actions)
    await expect(gitRadio).toBeChecked()
    await pressKey(page, 'ArrowUp', 'Select Directory registry after reopen', actions)
    await expect(directoryRadio).toBeChecked()
    await tabTo(page, location, 'Source location after reopen', actions, dialog)
    await typeKeyboard(page, sourceDir, 'private optional directory source after reopen', actions)
    await tabTo(page, addSource, 'Add source after reopen', actions, dialog)
    await pressKey(page, 'Enter', 'Queue optional source after reopen', actions)
    await expect(pendingSources).toContainText(sourceDir)
    expect(configurationActionCount()).toBe(0)

    const finish = dialog.getByRole('button', { name: 'Finish setup', exact: true })
    await expect(finish).toHaveAccessibleName('Finish setup')
    const initializeResponse = armPost(page, '/api/configuration/actions')
    await tabTo(page, finish, 'Finish setup button', actions, dialog)
    await pressKey(page, 'Enter', 'Finish setup', actions)
    const initialized = await completeResponse(initializeResponse, 202)
    const initializeRequest = initialized.request().postDataJSON() as {
      action: string
      targets: string[]
      consent: boolean
      sources: Array<{ kind: string; location: string }>
    }
    expect(initializeRequest.action).toBe('initialize')
    expect(initializeRequest.targets).toEqual(expect.arrayContaining(['factory', 'devin']))
    expect(initializeRequest.consent).toBe(false)
    expect(initializeRequest.sources).toEqual([{ kind: 'directory', location: sourceDir }])
    expect(configurationActionCount()).toBe(1)
    await expect(page.getByLabel('Notifications').getByText('mutation complete').last()).toBeVisible({ timeout: 60_000 })
    await expect(dialog).not.toBeVisible({ timeout: 60_000 })

    await expect.poll(async () => await readFile(userConfigPath, 'utf8'), { timeout: 60_000 }).toContain(sourceDir)
    const writtenRoot = await readFile(userConfigPath, 'utf8')
    expect(writtenRoot).toContain('factory')
    expect(writtenRoot).toContain('devin')
    expect(writtenRoot).not.toContain('level: basic')
    await expect(page.getByRole('option', { name: /solo/ })).toBeVisible({ timeout: 60_000 })
    expect((await readFile(repoConfigPath)).equals(repoBefore)).toBe(true)
    expect((await readFile(repoLocalPath)).equals(repoLocalBefore)).toBe(true)
    await expectOptionalFileUnchanged(personalLocalPath, personalLocalBefore)
    if (bootstrap.orgConfig != null && organizationBefore != null) {
      expect((await readFile(bootstrap.orgConfig)).equals(organizationBefore)).toBe(true)
    }

    await page.screenshot({ path: evidencePath(testInfo, 'first-run-no-telemetry-finished.png'), fullPage: true, animations: 'disabled' })
    actions.push('First-run cancellation preserved the absent root; explicit opt-out initialized targets and source')
  } finally {
    await harness.stop()
    await saveActions(testInfo, 'first-run.actions.json', actions)
  }
})
