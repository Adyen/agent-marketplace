import { expect, test, type Locator, type Page, type TestInfo } from '@playwright/test'
import { lstat, mkdir, readFile, realpath, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, isAbsolute, join, relative, sep } from 'node:path'
import { distIndexExists, launchBuiltUserApp } from './app'
import { startHarness, type Bootstrap } from './harness'
import { tabTo } from './keyboard'

interface Work40Bootstrap extends Bootstrap {
  work40Mode: string
  work40UserConfig: string
  work40UserFragment: string
  work40UserLock: string
  work40OrgConfig: string
  work40OrgLock: string
  work40Directory: string
  work40ReviewSource: string
  work40NextSource: string
  work40ReviewMaterialized: string
  work40NextMaterialized: string
  work40UnrelatedMaterialized: string
  resourceActionBarrierWaitURL: string
  resourceActionBarrierReleaseURL: string
}

interface FileState {
  path: string
  exists: boolean
  mode: number | null
  bytes: Buffer | null
}

type Work40Mode = 'root-owned' | 'source-included'

function isWithin(root: string, path: string): boolean {
  const relativePath = relative(root, path)
  return relativePath !== '..' && !relativePath.startsWith(`..${sep}`) && !isAbsolute(relativePath)
}

async function isWithinTemp(path: string): Promise<boolean> {
  const root = await realpath(tmpdir())
  let candidate = path
  for (;;) {
    try {
      return isWithin(root, await realpath(candidate))
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
        throw error
      }
      const parent = dirname(candidate)
      if (parent === candidate) {
        return false
      }
      candidate = parent
    }
  }
}

async function work40Bootstrap(value: Bootstrap): Promise<Work40Bootstrap> {
  const bootstrap = value as Work40Bootstrap
  const paths: [string, string | undefined][] = [
    ['home', bootstrap.home],
    ['repo', bootstrap.repo],
    ['userConfig', bootstrap.work40UserConfig],
    ['userFragment', bootstrap.work40UserFragment],
    ['userLock', bootstrap.work40UserLock],
    ['orgConfig', bootstrap.work40OrgConfig],
    ['orgLock', bootstrap.work40OrgLock],
    ['directory', bootstrap.work40Directory],
    ['reviewSource', bootstrap.work40ReviewSource],
    ['nextSource', bootstrap.work40NextSource],
    ['reviewMaterialized', bootstrap.work40ReviewMaterialized],
    ['nextMaterialized', bootstrap.work40NextMaterialized],
    ['unrelatedMaterialized', bootstrap.work40UnrelatedMaterialized],
  ]
  const invalidPaths = await Promise.all(paths.map(async ([name, path]) =>
    path == null || !isAbsolute(path) || !await isWithinTemp(path) ? name : null,
  ))
  const rejectedPaths = invalidPaths.filter((name): name is string => name != null)
  if (rejectedPaths.length > 0) {
    throw new Error(`Work40 fixture paths are not isolated absolute paths: ${rejectedPaths.join(', ')}`)
  }
  for (const endpoint of [bootstrap.url, bootstrap.resourceActionBarrierWaitURL, bootstrap.resourceActionBarrierReleaseURL]) {
    const url = new URL(endpoint)
    if (url.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(url.hostname)) {
      throw new Error('Work40 test endpoints must be loopback-only')
    }
  }
  return bootstrap
}

async function startWork40Harness(testInfo: TestInfo, mode: Work40Mode) {
  const outputDir = dirname(testInfo.outputPath('registry-resource-actions.actions.json'))
  const harnessLogDir = join(outputDir, 'harness')
  await mkdir(harnessLogDir, { recursive: true })
  return startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'work40-resource-actions',
    AM_LOCALAPI_USER_INITIAL_MODE: mode,
    AM_LOCALAPI_RELEASE_VERSION: 'dev',
    AM_WORK28_TELEMETRY_EVIDENCE_DIR: harnessLogDir,
  })
}

function protectedPaths(bootstrap: Work40Bootstrap): string[] {
  return [
    bootstrap.work40UserConfig,
    bootstrap.work40UserFragment,
    bootstrap.work40UserLock,
    bootstrap.work40OrgConfig,
    bootstrap.work40OrgLock,
    bootstrap.work40ReviewSource,
    bootstrap.work40NextSource,
    bootstrap.work40ReviewMaterialized,
    bootstrap.work40NextMaterialized,
    bootstrap.work40UnrelatedMaterialized,
    join(bootstrap.home!, '.agents', 'config.local.yaml'),
    join(bootstrap.repo!, '.agents', 'config.yaml'),
    join(bootstrap.repo!, '.agents', 'config.local.yaml'),
  ]
}

async function fileState(path: string): Promise<FileState> {
  try {
    const info = await lstat(path)
    return { path, exists: true, mode: info.mode, bytes: await readFile(path) }
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') {
      return { path, exists: false, mode: null, bytes: null }
    }
    throw error
  }
}

async function captureFiles(paths: string[]): Promise<FileState[]> {
  return Promise.all(paths.map(fileState))
}

function sameFileState(left: FileState, right: FileState): boolean {
  const sameBytes = left.bytes == null ? right.bytes == null : right.bytes != null && left.bytes.equals(right.bytes)
  return left.path === right.path && left.exists === right.exists && left.mode === right.mode && sameBytes
}

function assertFilesUnchanged(before: FileState[], after: FileState[], except: string[] = []): void {
  const ignored = new Set(except)
  if (before.length !== after.length) {
    throw new Error('Work40 protected file set changed')
  }
  for (let index = 0; index < before.length; index++) {
    if (!ignored.has(before[index]!.path) && !sameFileState(before[index]!, after[index]!)) {
      throw new Error('Work40 protected file bytes or mode changed')
    }
  }
}

function assertUserConfigMode(before: FileState[], path: string, after: FileState[]): void {
  const beforeState = before.find((file) => file.path === path)
  const afterState = after.find((file) => file.path === path)
  if (beforeState == null || afterState == null || !beforeState.exists || !afterState.exists || beforeState.mode !== afterState.mode) {
    throw new Error('Work40 user config mode changed')
  }
}

function assertNoLocalPaths(preview: string | null, bootstrap: Work40Bootstrap): void {
  if (preview != null && [bootstrap.home!, bootstrap.repo!, bootstrap.work40Directory].some((path) => preview.includes(path))) {
    throw new Error('Work40 static preview exposed a local path')
  }
}

async function saveActions(testInfo: TestInfo, actions: string[]): Promise<void> {
  const path = testInfo.outputPath('registry-resource-actions.actions.json')
  await mkdir(dirname(path), { recursive: true })
  await writeFile(path, `${JSON.stringify({ actions }, null, 2)}\n`, { mode: 0o600 })
}

async function launchWorkspace(page: Page, bootstrap: Work40Bootstrap, actions: string[]): Promise<void> {
  await page.setViewportSize({ width: 1280, height: 800 })
  await launchBuiltUserApp(page, bootstrap)
  await expect(page.getByRole('region', { name: 'Resources' })).toBeVisible({ timeout: 60_000 })
  const search = page.getByRole('button', { name: 'Search', exact: true })
  if (!(await search.evaluate((element) => element === document.activeElement))) {
    await tabTo(page, search, 'Search button', actions)
  }
}

async function selectReview(page: Page, actions: string[]): Promise<Locator> {
  const review = page.getByRole('option').filter({ hasText: 'review' }).first()
  await expect(review).toBeVisible({ timeout: 60_000 })
  await tabTo(page, review, 'user directory review row', actions)
  await page.keyboard.press('Space')
  await expect(review).toHaveAttribute('aria-selected', 'true')
  actions.push('Selected the user registry review row with Space')
  return review
}

async function selectedOptionDOMID(page: Page): Promise<string | null> {
  return page.getByRole('option').evaluateAll((elements) => {
    const selected = elements.find((element) => element.getAttribute('aria-selected') === 'true')
    return selected instanceof HTMLElement ? selected.id : null
  })
}

async function openPlan(page: Page, bootstrap: Work40Bootstrap, verb: 'Install' | 'Remove', actions: string[]): Promise<Locator> {
  const label = `${verb} Resource`
  const button = page.getByRole('button', { name: label, exact: true })
  await expect(button).toBeEnabled({ timeout: 30_000 })
  if (!(await button.evaluate((element) => element === document.activeElement))) {
    await tabTo(page, button, `${label} button`, actions)
  }
  const responsePromise = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/resource-plan')
  await page.keyboard.press('Enter')
  actions.push(`Opened ${label} with Enter`)
  const response = await responsePromise
  expect(response.status()).toBe(200)
  const dialog = page.getByRole('dialog', { name: label, exact: true })
  await expect(dialog).toBeVisible()
  await expect(dialog.locator('pre')).toBeVisible()
  assertNoLocalPaths(await dialog.locator('pre').textContent(), bootstrap)
  expect(await dialog.locator('script, img').count()).toBe(0)
  expect(await dialog.getByRole('checkbox').count()).toBe(0)
  return dialog
}

async function cancelPlan(page: Page, dialog: Locator, actions: string[]): Promise<void> {
  const cancel = dialog.getByRole('button', { name: 'Cancel', exact: true })
  await tabTo(page, cancel, 'Cancel resource action button', actions, dialog)
  await page.keyboard.press('Enter')
  await expect(dialog).toBeHidden()
  actions.push('Cancelled the static user registry plan with Enter')
}

async function confirmPlan(page: Page, dialog: Locator, verb: 'Install' | 'Remove', actions: string[]): Promise<void> {
  const button = dialog.getByRole('button', { name: `Confirm ${verb.toLowerCase()}`, exact: true })
  await expect(button).toBeEnabled()
  await tabTo(page, button, `Confirm ${verb.toLowerCase()} button`, actions, dialog)
  const responsePromise = page.waitForResponse((response) =>
    new URL(response.url()).pathname === '/api/resource-actions' && response.status() === 202,
  )
  await page.keyboard.press('Enter')
  actions.push(`Confirmed ${verb.toLowerCase()} with Enter`)
  const response = await responsePromise
  expect(response.status()).toBe(202)
}

function mutationStatus(page: Page): Locator {
  return page.getByRole('status').filter({ hasText: /^mutation(?:\s|$)/i })
}

function countActionRequests(page: Page) {
  const counts = { plans: 0, actions: 0, coreActions: 0 }
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path === '/api/resource-plan') {
      counts.plans++
    } else if (path === '/api/resource-actions') {
      counts.actions++
    } else if (path === '/api/actions' || path === '/api/configuration/actions') {
      counts.coreActions++
    }
  })
  return counts
}

function assertActionRequestCounts(counts: ReturnType<typeof countActionRequests>, plans: number, actions: number, message: string): void {
  if (counts.plans !== plans || counts.actions !== actions || counts.coreActions !== 0) {
    throw new Error(message)
  }
}

// spec: session-interactions/guarded-submission
// spec: session-navigation/clear-and-refresh
test('built GUI edits root-owned directory selections without SDK materialization or core actions', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the final GUI build before Work40 journeys')
  }
  const harness = await startWork40Harness(testInfo, 'root-owned')
  const actions: string[] = []
  const counts = countActionRequests(page)
  let bootstrapForCleanup: Work40Bootstrap | null = null
  let barrierReleased = false
  try {
    const bootstrap = await work40Bootstrap(harness.bootstrap)
    bootstrapForCleanup = bootstrap
    const paths = protectedPaths(bootstrap)
    const beforeCancel = await captureFiles(paths)
    await launchWorkspace(page, bootstrap, actions)
    const selectedReviewOption = await selectReview(page, actions)
    const reviewDOMID = await selectedReviewOption.getAttribute('id')
    if (reviewDOMID == null) {
      throw new Error('selected review row had no opaque DOM id')
    }
    const reviewByID = page.locator(`[id="${reviewDOMID}"]`)
    await expect.poll(() => selectedOptionDOMID(page)).toBe(reviewDOMID)
    await expect(reviewByID).toHaveAttribute('aria-selected', 'true')

    const cancelDialog = await openPlan(page, bootstrap, 'Install', actions)
    const preview = await cancelDialog.locator('pre').textContent()
    assertNoLocalPaths(preview, bootstrap)
    await cancelDialog.screenshot({ path: testInfo.outputPath('work40-directory-install-preview.png'), animations: 'disabled' })
    await cancelPlan(page, cancelDialog, actions)
    assertFilesUnchanged(beforeCancel, await captureFiles(paths))
    assertActionRequestCounts(counts, 1, 0, 'cancelling the static directory plan submitted a mutation')
    actions.push('Verified cancellation preserved all captured bytes/modes and sent no resource-actions request')
    await expect.poll(() => selectedOptionDOMID(page)).toBe(reviewDOMID)

    const installDialog = await openPlan(page, bootstrap, 'Install', actions)
    await expect(reviewByID).toHaveAttribute('aria-selected', 'true')
    const beforeInstall = await captureFiles(paths)
    const installPreview = await installDialog.locator('pre').textContent()
    assertNoLocalPaths(installPreview, bootstrap)
    await confirmPlan(page, installDialog, 'Install', actions)
    const heldFinal = await page.request.get(bootstrap.resourceActionBarrierWaitURL)
    expect(heldFinal.status()).toBe(204)
    await expect(installDialog).toBeHidden()
    await expect(mutationStatus(page)).toBeVisible()
    const afterInstallBeforeFinal = await captureFiles(paths)
    assertFilesUnchanged(beforeInstall, afterInstallBeforeFinal, [bootstrap.work40UserConfig])
    assertUserConfigMode(beforeInstall, bootstrap.work40UserConfig, afterInstallBeforeFinal)
    if (sameFileState(beforeInstall[0]!, afterInstallBeforeFinal[0]!)) {
      throw new Error('the accepted user directory install did not update the user selector')
    }
    actions.push('Observed the real user selector write while the final event was held; lock, source, organization, CWD, and materialized files remained unchanged')
    await saveActions(testInfo, actions)

    const releaseInstall = await page.request.post(bootstrap.resourceActionBarrierReleaseURL)
    expect(releaseInstall.status()).toBe(204)
    barrierReleased = true
    await expect(mutationStatus(page)).toBeHidden({ timeout: 60_000 })
    await expect.poll(() => selectedOptionDOMID(page)).toBe(reviewDOMID)
    await expect(reviewByID).toBeVisible()
    await expect(reviewByID).toHaveAttribute('aria-selected', 'true')
    const removeButton = page.getByRole('button', { name: 'Remove Resource', exact: true })
    await expect(removeButton).toBeFocused()
    const visibleOrderBeforeRemove = await page.getByRole('option').evaluateAll((elements) => elements.map((element) => (element as HTMLElement).id))
    const reviewIndex = visibleOrderBeforeRemove.indexOf(reviewDOMID)
    if (reviewIndex < 0) {
      throw new Error('captured review id was not in the pre-remove visible order')
    }
    const expectedNextSelectedID = visibleOrderBeforeRemove.slice(reviewIndex + 1).find((id) => id !== reviewDOMID) ??
      visibleOrderBeforeRemove.slice(0, reviewIndex).reverse().find((id) => id !== reviewDOMID)
    if (expectedNextSelectedID == null) {
      throw new Error('the root-owned fixture has no neighboring visible row for removal selection')
    }

    const removeDialog = await openPlan(page, bootstrap, 'Remove', actions)
    await expect(reviewByID).toHaveAttribute('aria-selected', 'true')
    const beforeRemove = await captureFiles(paths)
    await confirmPlan(page, removeDialog, 'Remove', actions)
    await expect(mutationStatus(page)).toBeHidden({ timeout: 60_000 })
    const afterRemove = await captureFiles(paths)
    assertFilesUnchanged(beforeRemove, afterRemove, [bootstrap.work40UserConfig])
    assertUserConfigMode(beforeRemove, bootstrap.work40UserConfig, afterRemove)
    await expect(reviewByID).toBeVisible()
    await expect(reviewByID).toHaveAttribute('id', reviewDOMID)
    await expect(reviewByID).toHaveAttribute('aria-selected', 'false')
    await expect.poll(() => selectedOptionDOMID(page)).toBe(expectedNextSelectedID)
    actions.push('Verified removal advanced selection to the nearest surviving row in the prior visible order')

    await expect(reviewByID).toBeVisible()
    await tabTo(page, reviewByID, 'captured user directory review row', actions)
    await page.keyboard.press('Space')
    await expect(reviewByID).toHaveAttribute('aria-selected', 'true')
    actions.push('Reselected the captured Review row by its original opaque DOM id')
    await expect(page.getByRole('button', { name: 'Install Resource', exact: true })).toBeEnabled()
    assertActionRequestCounts(counts, 3, 2, 'Work40 journey used an unexpected plan, mutation, or core-action endpoint')
    actions.push('Confirmed removal through the real user API; the current review row is excluded and no core action/materialization ran')
    await saveActions(testInfo, actions)
  } finally {
    if (!barrierReleased && bootstrapForCleanup != null) {
      await page.request.post(bootstrapForCleanup.resourceActionBarrierReleaseURL).catch(() => undefined)
    }
    await page.close().catch(() => undefined)
    await harness.stop()
  }
})

// spec: session-interactions/guarded-submission
test('built GUI refuses a directory source owned only by an included user fragment', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the final GUI build before Work40 journeys')
  }
  const harness = await startWork40Harness(testInfo, 'source-included')
  const actions: string[] = []
  const counts = countActionRequests(page)
  let bootstrapForCleanup: Work40Bootstrap | null = null
  let barrierReleased = false
  try {
    const bootstrap = await work40Bootstrap(harness.bootstrap)
    bootstrapForCleanup = bootstrap
    const paths = protectedPaths(bootstrap)
    const beforeCancel = await captureFiles(paths)
    await launchWorkspace(page, bootstrap, actions)
    await selectReview(page, actions)

    const cancelDialog = await openPlan(page, bootstrap, 'Remove', actions)
    await cancelPlan(page, cancelDialog, actions)
    assertFilesUnchanged(beforeCancel, await captureFiles(paths))
    assertActionRequestCounts(counts, 1, 0, 'cancelling the included-owner plan submitted a mutation')
    actions.push('Cancelled the included-owner removal plan without writes')

    const removeDialog = await openPlan(page, bootstrap, 'Remove', actions)
    const beforeRemove = await captureFiles(paths)
    await confirmPlan(page, removeDialog, 'Remove', actions)
    const heldFinal = await page.request.get(bootstrap.resourceActionBarrierWaitURL)
    expect(heldFinal.status()).toBe(204)
    await expect(removeDialog).toBeHidden()
    await expect(mutationStatus(page)).toBeVisible()
    assertFilesUnchanged(beforeRemove, await captureFiles(paths))
    const release = await page.request.post(bootstrap.resourceActionBarrierReleaseURL)
    expect(release.status()).toBe(204)
    barrierReleased = true
    await expect(mutationStatus(page)).toBeHidden({ timeout: 60_000 })
    await expect(page.getByLabel('Notifications').getByText('mutation failed')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Remove Resource', exact: true })).toBeFocused()
    assertFilesUnchanged(beforeRemove, await captureFiles(paths))
    assertActionRequestCounts(counts, 2, 1, 'included-owner refusal used an unexpected API endpoint')
    actions.push('Observed the included-source refusal and final error; user root, fragment, org, locks, source files, CWD, and materialized files stayed unchanged')
    await saveActions(testInfo, actions)
  } finally {
    if (!barrierReleased && bootstrapForCleanup != null) {
      await page.request.post(bootstrapForCleanup.resourceActionBarrierReleaseURL).catch(() => undefined)
    }
    await page.close().catch(() => undefined)
    await harness.stop()
  }
})
