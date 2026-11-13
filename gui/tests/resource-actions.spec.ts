import { expect, test, type Locator, type Page, type TestInfo } from '@playwright/test'
import { lstat, mkdir, readFile, realpath, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, isAbsolute, join, relative, sep } from 'node:path'
import { distIndexExists, launchBuiltUserApp } from './app'
import { startHarness, type Bootstrap } from './harness'
import { focusedDescription, tabTo } from './keyboard'

interface Work39Bootstrap extends Bootstrap {
  marketplaceEndpoint: string
  resourceActionUserConfig: string
  resourceActionUserLock: string
  resourceActionOrgConfig: string
  resourceActionOrgLock: string
  resourceActionMaterialized: string
  resourceActionUnrelatedMaterialized: string
  resourceActionBarrierWaitURL: string
  resourceActionBarrierReleaseURL: string
}

interface FileState {
  exists: boolean
  mode: number | null
  bytes: Buffer | null
}

type Work39InitialMode = 'available' | 'user-installed' | 'org-optional'

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

async function work39Bootstrap(bootstrap: Bootstrap): Promise<Work39Bootstrap> {
  const value = bootstrap as Work39Bootstrap
  const paths: [string, string | undefined][] = [
    ['home', value.home],
    ['repo', value.repo],
    ['orgConfig', value.orgConfig],
    ['userConfig', value.resourceActionUserConfig],
    ['userLock', value.resourceActionUserLock],
    ['orgConfigPath', value.resourceActionOrgConfig],
    ['orgLock', value.resourceActionOrgLock],
    ['materialized', value.resourceActionMaterialized],
    ['unrelatedMaterialized', value.resourceActionUnrelatedMaterialized],
  ]
  const invalidPaths = await Promise.all(paths.map(async ([name, path]) =>
    path == null || !isAbsolute(path) || !await isWithinTemp(path) ? name : null,
  ))
  const rejectedPaths = invalidPaths.filter((name): name is string => name != null)
  if (rejectedPaths.length > 0) {
    throw new Error(`Work39 harness fixture paths are not isolated absolute paths: ${rejectedPaths.join(', ')}`)
  }
  for (const endpoint of [value.marketplaceEndpoint, value.resourceActionBarrierWaitURL, value.resourceActionBarrierReleaseURL]) {
    const url = new URL(endpoint)
    if (url.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(url.hostname)) {
      throw new Error('Work39 test endpoints must be loopback-only')
    }
  }
  return value
}

async function startWork39Harness(testInfo: TestInfo, mode: Work39InitialMode, risk: boolean) {
  const outputDir = dirname(testInfo.outputPath('resource-actions.actions.json'))
  if (!isAbsolute(outputDir)) {
    throw new Error('resource-actions evidence directory must be absolute')
  }
  const harnessLogDir = join(outputDir, 'harness')
  await mkdir(harnessLogDir, { recursive: true })
  return startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'work39-resource-actions',
    AM_LOCALAPI_USER_INITIAL_MODE: mode,
    AM_LOCALAPI_USER_ORG_REQUIRED: '0',
    AM_LOCALAPI_USER_RISK: risk ? '1' : '0',
    AM_LOCALAPI_RELEASE_VERSION: 'dev',
    AM_WORK28_TELEMETRY_EVIDENCE_DIR: harnessLogDir,
  })
}

async function saveActions(testInfo: TestInfo, actions: string[]): Promise<void> {
  const path = testInfo.outputPath('resource-actions.actions.json')
  await mkdir(dirname(path), { recursive: true })
  await writeFile(path, `${JSON.stringify({ actions }, null, 2)}\n`, { mode: 0o600 })
}

function allProtectedPaths(bootstrap: Work39Bootstrap): string[] {
  return [
    bootstrap.resourceActionUserConfig,
    bootstrap.resourceActionUserLock,
    bootstrap.resourceActionOrgConfig,
    bootstrap.resourceActionOrgLock,
    bootstrap.resourceActionMaterialized,
    bootstrap.resourceActionUnrelatedMaterialized,
    join(bootstrap.home!, '.agents', 'config.local.yaml'),
    join(bootstrap.home!, '.agents', 'fragment.yaml'),
    join(bootstrap.repo!, '.agents', 'config.yaml'),
    join(bootstrap.repo!, '.agents', 'config.local.yaml'),
  ]
}

function protectedOutsideUserMutation(bootstrap: Work39Bootstrap): string[] {
  return [
    bootstrap.resourceActionOrgConfig,
    bootstrap.resourceActionOrgLock,
    bootstrap.resourceActionUnrelatedMaterialized,
    join(bootstrap.home!, '.agents', 'config.local.yaml'),
    join(bootstrap.home!, '.agents', 'fragment.yaml'),
    join(bootstrap.repo!, '.agents', 'config.yaml'),
    join(bootstrap.repo!, '.agents', 'config.local.yaml'),
  ]
}

function protectedOrganizationMutation(bootstrap: Work39Bootstrap): string[] {
  return [bootstrap.resourceActionUserLock, bootstrap.resourceActionMaterialized, ...protectedOutsideUserMutation(bootstrap)]
}

async function fileState(path: string): Promise<FileState> {
  try {
    const info = await lstat(path)
    return { exists: true, mode: info.mode, bytes: await readFile(path) }
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') {
      return { exists: false, mode: null, bytes: null }
    }
    throw error
  }
}

async function captureFiles(paths: string[]): Promise<FileState[]> {
  return Promise.all(paths.map(fileState))
}

function sameFileState(left: FileState, right: FileState): boolean {
  const bytesEqual = left.bytes == null ? right.bytes == null : right.bytes != null && left.bytes.equals(right.bytes)
  return left.exists === right.exists && left.mode === right.mode && bytesEqual
}

function assertFilesUnchanged(before: FileState[], after: FileState[], message: string): void {
  if (before.length !== after.length || before.some((file, index) => !sameFileState(file, after[index]!))) {
    throw new Error(message)
  }
}

function assertPreviewContainsNoLocalPaths(preview: string | null, paths: string[]): void {
  if (preview != null && paths.some((path) => preview.includes(path))) {
    throw new Error('resource action preview exposed a local path')
  }
}

function countRequests(page: Page) {
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

function mutationStatus(page: Page): Locator {
  return page.locator('.gui-toast--progress')
}

async function expectNoInternalFeedback(locator: Locator): Promise<void> {
  expect(await locator.innerText()).not.toMatch(/resource action|mutation|Working…/i)
}

async function trackOpenedDialogs(page: Page): Promise<void> {
  await page.evaluate(() => {
    const root = document.documentElement
    root.dataset.openedDialogObserved = 'false'
    const check = () => {
      const visible = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')].some((dialog) => {
        const style = getComputedStyle(dialog)
        const bounds = dialog.getBoundingClientRect()
        return style.display !== 'none' && style.visibility !== 'hidden' && bounds.width > 0 && bounds.height > 0
      })
      if (visible) {
        root.dataset.openedDialogObserved = 'true'
      }
    }
    const observer = new MutationObserver(check)
    observer.observe(document.body, { attributes: true, childList: true, characterData: true, subtree: true })
    ;(window as typeof window & { __resourceActionDialogObserver?: MutationObserver }).__resourceActionDialogObserver = observer
    check()
  })
}

async function stopDialogTracking(page: Page): Promise<boolean> {
  return page.evaluate(() => {
    const trackedWindow = window as typeof window & { __resourceActionDialogObserver?: MutationObserver }
    trackedWindow.__resourceActionDialogObserver?.disconnect()
    delete trackedWindow.__resourceActionDialogObserver
    return document.documentElement.dataset.openedDialogObserved === 'true'
  })
}

async function launchWorkspace(page: Page, bootstrap: Work39Bootstrap, actions: string[]): Promise<Locator> {
  await page.setViewportSize({ width: 1280, height: 800 })
  await launchBuiltUserApp(page, bootstrap)
  const resources = page.getByRole('region', { name: 'Resources' })
  await expect(resources).toBeVisible({ timeout: 60_000 })
  const search = page.getByRole('button', { name: 'Search', exact: true })
  const initialFocus = await focusedDescription(page)
  if (initialFocus !== 'button "Search"') {
    const navigationKey = initialFocus === '<none>' || initialFocus === 'body' ? 'Tab' : 'Shift+Tab'
    await tabTo(page, search, 'Search button', actions, undefined, navigationKey)
  }
  return resources
}

async function selectReviewRow(page: Page, actions: string[]): Promise<Locator> {
  const review = page.getByRole('option').filter({ hasText: 'review' }).first()
  await expect(review).toBeVisible({ timeout: 60_000 })
  await tabTo(page, review, 'Marketplace review row', actions)
  await page.keyboard.press('Space')
  await expect(review).toHaveAttribute('aria-selected', 'true')
  actions.push('Selected the Marketplace review row with Space')
  return review
}

async function selectRowByDOMID(page: Page, domID: string, actions: string[]): Promise<void> {
  const options = page.getByRole('option')
  const index = await options.evaluateAll((elements, expected) =>
    elements.findIndex((element) => (element as HTMLElement).id === expected), domID)
  if (index < 0) {
    throw new Error('The organization-owned Marketplace row was not present after the final snapshot')
  }
  const row = options.nth(index)
  await tabTo(page, row, 'Organization-owned Marketplace row', actions)
  await page.keyboard.press('Space')
  await expect(row).toHaveAttribute('aria-selected', 'true')
  actions.push('Selected the organization-owned Marketplace row with Space')
}

async function openActionPlan(page: Page, verb: string, actions: string[]): Promise<Locator> {
  const label = `${verb} Resource`
  const actionButton = page.getByRole('button', { name: label, exact: true })
  await expect(actionButton).toBeEnabled({ timeout: 30_000 })
  const responsePromise = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/resource-plan')
  if (!(await actionButton.evaluate((element) => element === document.activeElement))) {
    await tabTo(page, actionButton, `${label} button`, actions)
  }
  await page.keyboard.press('Enter')
  actions.push(`Opened ${label} with Enter`)
  const response = await responsePromise
  expect(response.status()).toBe(200)
  const dialog = page.getByRole('dialog', { name: label, exact: true })
  await expect(dialog).toBeVisible()
  await dialog.getByText('Technical details', { exact: true }).click()
  await expect(dialog.locator('pre')).toBeVisible()
  expect(await dialog.locator('script, img').count()).toBe(0)
  return dialog
}

async function confirmAction(page: Page, dialog: Locator, verb: string, actions: string[]): Promise<void> {
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

async function scrollRiskPreviewWithKeyboard(page: Page, dialog: Locator, testInfo: TestInfo, actions: string[]): Promise<void> {
  const preview = dialog.getByRole('region', { name: 'Action preview', exact: true })
  await expect(preview).toBeVisible()
  const before = await preview.evaluate((element) => ({ scrollHeight: element.scrollHeight, clientHeight: element.clientHeight, scrollTop: element.scrollTop }))
  if (before.scrollHeight <= before.clientHeight) {
    throw new Error('risk preview fixture did not overflow its scroll region')
  }
  await tabTo(page, preview, 'Resource action preview region', actions, dialog)
  await page.keyboard.press('End')
  await expect.poll(() => preview.evaluate((element) => element.scrollTop)).toBeGreaterThan(before.scrollTop)
  const findingVisible = await preview.evaluate((element, needle) => {
    if (!element.textContent?.includes(needle)) {
      return false
    }
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT)
    let textNode: Node | null
    while ((textNode = walker.nextNode()) != null) {
      const content = textNode.textContent ?? ''
      const start = content.indexOf(needle)
      if (start < 0) {
        continue
      }
      const range = document.createRange()
      range.setStart(textNode, start)
      range.setEnd(textNode, start + needle.length)
      const bounds = element.getBoundingClientRect()
      return Array.from(range.getClientRects()).some((rect) => rect.top >= bounds.top && rect.bottom <= bounds.bottom)
    }
    return false
  }, 'Fixture content requires explicit acknowledgement')
  if (!findingVisible) {
    throw new Error('risk finding text was not visible after keyboard scrolling')
  }
  await preview.screenshot({ path: testInfo.outputPath('risk-preview-after-keyboard-scroll.png'), animations: 'disabled' })
  actions.push('Scrolled the overflowing risk preview with End; the risk finding is visible and the preview remains plain text')
}

async function cancelAction(page: Page, dialog: Locator, actions: string[]): Promise<void> {
  const button = dialog.getByRole('button', { name: 'Cancel', exact: true })
  await tabTo(page, button, 'Cancel resource action button', actions, dialog)
  await page.keyboard.press('Enter')
  await expect(dialog).toBeHidden()
  actions.push('Cancelled the prepared resource action with Enter')
}

// spec: gui-shell/spinner-shows-only-while-the-action-is-in-flight
// spec: gui-shell/progress-names-the-action-without-invented-phases
test('resource action dialog shows a spinner while preparing the plan', async ({ page }, testInfo) => {
  const harness = await startWork39Harness(testInfo, 'user-installed', false)
  let releaseResponse!: () => void
  let responseFetched!: () => void
  const heldResponse = new Promise<void>((resolve) => {
    releaseResponse = resolve
  })
  const fetched = new Promise<void>((resolve) => {
    responseFetched = resolve
  })
  try {
    const bootstrap = await work39Bootstrap(harness.bootstrap)
    await launchWorkspace(page, bootstrap, [])
    await selectReviewRow(page, [])
    await page.route('**/api/resource-plan', async (route) => {
      const response = await route.fetch()
      responseFetched()
      await heldResponse
      await route.fulfill({ response })
    })
    await page.getByRole('button', { name: 'Remove Resource', exact: true }).click()
    await fetched
    const dialog = page.getByRole('dialog', { name: 'Remove Resource', exact: true })
    await expect(dialog).toBeVisible()
    await expect(dialog.locator('.resource-actions__spinner')).toBeVisible()
    await expect.poll(() => dialog.evaluate((element) => getComputedStyle(element).cursor)).toBe('progress')
    await expect.poll(() => dialog.locator('button:disabled').evaluateAll((buttons) => buttons.every((button) => getComputedStyle(button).cursor !== 'pointer'))).toBe(true)
    const status = dialog.locator('.resource-actions__status')
    await expect(status).toHaveText('Checking review…')
    await expectNoInternalFeedback(status)
    releaseResponse()
    await expect(dialog.locator('.resource-actions__spinner')).toHaveCount(0)
  } finally {
    releaseResponse()
    await page.close().catch(() => undefined)
    await harness.stop()
  }
})

// spec: gui-shell/plain-install-and-restore-skip-the-dialog
// spec: gui-shell/progress-names-the-action-without-invented-phases
test('risk-free Install writes without opening a dialog and shows busy inspector feedback', async ({ page }, testInfo) => {
  const harness = await startWork39Harness(testInfo, 'available', false)
  const counts = countRequests(page)
  const actions: string[] = []
  let releasePlan!: () => void
  let planFetched!: () => void
  let releasePost!: () => void
  let postFetched!: () => void
  let bootstrapForCleanup: Work39Bootstrap | null = null
  let barrierReleased = false
  const heldPlan = new Promise<void>((resolve) => { releasePlan = resolve })
  const planReady = new Promise<void>((resolve) => { planFetched = resolve })
  const heldPost = new Promise<void>((resolve) => { releasePost = resolve })
  const postReady = new Promise<void>((resolve) => { postFetched = resolve })
  try {
    const bootstrap = await work39Bootstrap(harness.bootstrap)
    bootstrapForCleanup = bootstrap
    const protectedPaths = allProtectedPaths(bootstrap)
    const userConfigIndex = protectedPaths.indexOf(bootstrap.resourceActionUserConfig)
    const userLockIndex = protectedPaths.indexOf(bootstrap.resourceActionUserLock)
    const protectedBefore = await captureFiles(protectedPaths)
    const outsideBefore = await captureFiles(protectedOutsideUserMutation(bootstrap))
    await launchWorkspace(page, bootstrap, actions)
    await selectReviewRow(page, actions)
    await trackOpenedDialogs(page)
    await page.route('**/api/resource-plan', async (route) => {
      const response = await route.fetch()
      planFetched()
      await heldPlan
      await route.fulfill({ response })
    })
    await page.route('**/api/resource-actions', async (route) => {
      const response = await route.fetch()
      postFetched()
      await heldPost
      await route.fulfill({ response })
    })

    const installButton = page.getByRole('button', { name: 'Install Resource', exact: true })
    await installButton.click()
    await planReady
    await expect(installButton).toBeDisabled()
    await expect(installButton.locator('.gui-inspector__action-spinner')).toBeVisible()
    await expect.poll(() => installButton.evaluate((element) => getComputedStyle(element).cursor)).toBe('progress')
    expect(await stopDialogTracking(page)).toBe(false)
    await expect(page.getByRole('dialog')).toHaveCount(0)

    await trackOpenedDialogs(page)
    releasePlan()
    await postReady
    await expect(installButton).toBeDisabled()
    await expect(installButton.locator('.gui-inspector__action-spinner')).toBeVisible()
    await expect.poll(() => installButton.evaluate((element) => getComputedStyle(element).cursor)).toBe('progress')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    releasePost()

    const heldFinal = await page.request.get(bootstrap.resourceActionBarrierWaitURL)
    expect(heldFinal.status()).toBe(204)
    const progress = mutationStatus(page)
    await expect(progress.locator('.gui-toast__title')).toHaveText('Installing review…')
    await expectNoInternalFeedback(progress)
    const installedFiles = await captureFiles(protectedPaths)
    if (
      userConfigIndex < 0 || userLockIndex < 0 ||
      sameFileState(protectedBefore[userConfigIndex]!, installedFiles[userConfigIndex]!) ||
      sameFileState(protectedBefore[userLockIndex]!, installedFiles[userLockIndex]!)
    ) {
      throw new Error('risk-free install did not write both user selection and acquisition lock')
    }
    assertFilesUnchanged(outsideBefore, await captureFiles(protectedOutsideUserMutation(bootstrap)), 'risk-free install changed organization or unrelated protected files')
    const release = await page.request.post(bootstrap.resourceActionBarrierReleaseURL)
    expect(release.status()).toBe(204)
    barrierReleased = true
    await expect(page.getByLabel('Notifications').getByText('Installed review')).toBeVisible({ timeout: 60_000 })
    expect(await stopDialogTracking(page)).toBe(false)
    await expect(page.getByRole('dialog')).toHaveCount(0)
    expect(counts.plans).toBe(1)
    expect(counts.actions).toBe(1)
    expect(counts.coreActions).toBe(0)
    actions.push('Risk-free Install wrote the user selection and lock through one plan/action pair without opening a dialog')
  } finally {
    releasePlan()
    releasePost()
    if (!barrierReleased && bootstrapForCleanup != null) {
      await page.request.post(bootstrapForCleanup.resourceActionBarrierReleaseURL).catch(() => undefined)
    }
    await page.close().catch(() => undefined)
    await harness.stop()
  }
})

// spec: gui-shell/modal-title-and-close-align-in-both-themes
test('non-bare modal title and close button share a vertical center in both themes', async ({ page }, testInfo) => {
  const harness = await startWork39Harness(testInfo, 'available', true)
  try {
    const bootstrap = await work39Bootstrap(harness.bootstrap)
    await page.addInitScript(() => {
      if (localStorage.getItem('vitepress-theme-appearance') === null) {
        localStorage.setItem('vitepress-theme-appearance', 'light')
      }
    })
    await launchWorkspace(page, bootstrap, [])
    for (const theme of ['light', 'dark']) {
      if (theme === 'dark') {
        await page.evaluate(() => localStorage.setItem('vitepress-theme-appearance', 'dark'))
        await page.reload()
        await expect(page.locator('html')).toHaveClass(/(?:^|\s)dark(?:\s|$)/)
        await expect(page.getByRole('region', { name: 'Resources' })).toBeVisible({ timeout: 60_000 })
      }
      await selectReviewRow(page, [])
      const dialog = await openActionPlan(page, 'Install', [])
      const title = dialog.locator('.gui-modal__title')
      const close = dialog.getByRole('button', { name: 'Close dialog', exact: true })
      const centers = await Promise.all([title, close].map((element) =>
        element.evaluate((node) => {
          const box = node.getBoundingClientRect()
          return box.top + box.height / 2
        }),
      ))
      expect(Math.abs(centers[0]! - centers[1]!), `${theme} modal title and close alignment`).toBeLessThanOrEqual(1)
      await cancelAction(page, dialog, [])
    }
  } finally {
    await page.close().catch(() => undefined)
    await harness.stop()
  }
})

// spec: session-interactions/guarded-submission
// spec: gui-shell/risk-and-preparation-failure-keep-the-dialog
// spec: gui-shell/remove-and-exclude-keep-confirmation
// spec: gui-shell/progress-names-the-action-without-invented-phases
test('built user GUI cancels, installs, and removes a warned Marketplace Resource through authoritative finals', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the resource-action journey')
  }
  const harness = await startWork39Harness(testInfo, 'available', true)
  const actions: string[] = []
  const counts = countRequests(page)
  let releaseActionResponse!: () => void
  let actionResponseFetched!: () => void
  const heldActionResponse = new Promise<void>((resolve) => { releaseActionResponse = resolve })
  const actionResponseReady = new Promise<void>((resolve) => { actionResponseFetched = resolve })
  let bootstrapForCleanup: Work39Bootstrap | null = null
  let barrierReleased = false
  try {
    const bootstrap = await work39Bootstrap(harness.bootstrap)
    bootstrapForCleanup = bootstrap
    const rootPaths = allProtectedPaths(bootstrap)
    const userConfigIndex = rootPaths.indexOf(bootstrap.resourceActionUserConfig)
    const userLockIndex = rootPaths.indexOf(bootstrap.resourceActionUserLock)
    const materializedIndex = rootPaths.indexOf(bootstrap.resourceActionMaterialized)
    const beforeCancel = await captureFiles(rootPaths)
    const beforeNonUserMutation = await captureFiles(protectedOutsideUserMutation(bootstrap))
    await launchWorkspace(page, bootstrap, actions)
    await selectReviewRow(page, actions)

    const cancelDialog = await openActionPlan(page, 'Install', actions)
    const preview = await cancelDialog.locator('pre').textContent()
    assertPreviewContainsNoLocalPaths(preview, [bootstrap.home!, bootstrap.repo!])
    await cancelDialog.screenshot({ path: testInfo.outputPath('available-install-preview.png'), animations: 'disabled' })
    await cancelAction(page, cancelDialog, actions)
    await expect(page.getByRole('button', { name: 'Install Resource', exact: true })).toBeFocused()
    assertFilesUnchanged(beforeCancel, await captureFiles(rootPaths), 'cancelled prepare modified protected fixture files')
    expect(counts.plans).toBe(1)
    expect(counts.actions).toBe(0)
    actions.push('Verified Cancel sent no resource-actions request and preserved all captured fixture bytes and modes')
    await saveActions(testInfo, actions)

    const installDialog = await openActionPlan(page, 'Install', actions)
    const installPreview = await installDialog.locator('pre').textContent()
    assertPreviewContainsNoLocalPaths(installPreview, [bootstrap.home!, bootstrap.repo!])
    await scrollRiskPreviewWithKeyboard(page, installDialog, testInfo, actions)
    const beforeInstallCommit = await captureFiles(rootPaths)
    const beforeInstallNonUser = await captureFiles(protectedOutsideUserMutation(bootstrap))
    const risk = installDialog.getByRole('checkbox')
    const installConfirm = installDialog.getByRole('button', { name: 'Confirm install', exact: true })
    await expect(risk).not.toBeChecked()
    await expect(installConfirm).toBeDisabled()
    await tabTo(page, risk, 'Marketplace risk acknowledgement checkbox', actions, installDialog)
    await page.keyboard.press('Space')
    await expect(risk).toBeChecked()
    await expect(installConfirm).toBeEnabled()
    actions.push('Explicitly acknowledged the warned Marketplace install; the checkbox began unselected')
    await saveActions(testInfo, actions)

    const installResponsePromise = page.waitForResponse((response) =>
      new URL(response.url()).pathname === '/api/resource-actions' && response.status() === 202,
    )
    await page.route('**/api/resource-actions', async (route) => {
      const response = await route.fetch()
      actionResponseFetched()
      await heldActionResponse
      await route.fulfill({ response })
    })
    await tabTo(page, installConfirm, 'Confirm install button', actions, installDialog)
    await page.keyboard.press('Enter')
    actions.push('Submitted the risk-acknowledged install through the real Local API')
    await actionResponseReady
    await expect(installDialog).toBeVisible()
    await expect(installDialog.locator('.resource-actions__spinner')).toBeVisible()
    await expect.poll(() => installDialog.evaluate((element) => getComputedStyle(element).cursor)).toBe('progress')
    await expect.poll(() => installDialog.locator('button:disabled').evaluateAll((buttons) => buttons.every((button) => getComputedStyle(button).cursor !== 'pointer'))).toBe(true)
    await expect(installDialog.locator('.resource-actions__status')).toHaveText('Installing review…')
    await expectNoInternalFeedback(installDialog)
    releaseActionResponse()
    const installResponse = await installResponsePromise
    expect(installResponse.status()).toBe(202)
    const heldFinal = await page.request.get(bootstrap.resourceActionBarrierWaitURL)
    expect(heldFinal.status()).toBe(204)
    await expect(installDialog).toBeHidden()
    await expect(mutationStatus(page)).toBeVisible()
    const oldInstallButton = page.getByRole('button', { name: 'Install Resource', exact: true })
    await expect(oldInstallButton).toBeDisabled()
    await oldInstallButton.screenshot({ path: testInfo.outputPath('install-awaiting-authoritative-final.png'), animations: 'disabled' })
    const afterCommitBeforeFinal = await captureFiles(rootPaths)
    if (
      materializedIndex < 0 || !afterCommitBeforeFinal[materializedIndex]?.exists ||
      sameFileState(beforeInstallCommit[materializedIndex]!, afterCommitBeforeFinal[materializedIndex]!)
    ) {
      throw new Error('accepted install did not materialize its actual Devin UserRoot target before final delivery')
    }
    if (
      userConfigIndex < 0 || userLockIndex < 0 || sameFileState(beforeInstallCommit[userConfigIndex]!, afterCommitBeforeFinal[userConfigIndex]!) ||
      sameFileState(beforeInstallCommit[userLockIndex]!, afterCommitBeforeFinal[userLockIndex]!)
    ) {
      throw new Error('accepted install did not update the user config and acquisition lock before final delivery')
    }
    assertFilesUnchanged(
      beforeInstallNonUser,
      await captureFiles(protectedOutsideUserMutation(bootstrap)),
      'accepted install changed organization, CWD, or unrelated materialized bytes',
    )
    actions.push('Observed the actual Devin UserRoot materialization while the authoritative final remained held')
    await saveActions(testInfo, actions)

    const release = await page.request.post(bootstrap.resourceActionBarrierReleaseURL)
    expect(release.status()).toBe(204)
    barrierReleased = true
    const removeButton = page.getByRole('button', { name: 'Remove Resource', exact: true })
    await expect(removeButton).toBeVisible({ timeout: 60_000 })
    await expect(removeButton).toBeFocused()
    await removeButton.screenshot({ path: testInfo.outputPath('installed-authoritative-final.png'), animations: 'disabled' })
    actions.push('Released the real final event; the verified installed row now offers Remove Resource with focus restored')

    const beforeRemoveConfirmation = await captureFiles(rootPaths)
    const removeDialog = await openActionPlan(page, 'Remove', actions)
    const removePreview = await removeDialog.locator('pre').textContent()
    assertPreviewContainsNoLocalPaths(removePreview, [bootstrap.home!, bootstrap.repo!])
    assertFilesUnchanged(beforeRemoveConfirmation, await captureFiles(rootPaths), 'opening removal modified protected fixture files before confirmation')
    await removeDialog.screenshot({ path: testInfo.outputPath('remove-preview.png'), animations: 'disabled' })
    await confirmAction(page, removeDialog, 'Remove', actions)
    await expect(removeDialog).toBeHidden()
    await expect(mutationStatus(page)).toBeHidden({ timeout: 60_000 })
    await selectReviewRow(page, actions)
    const installButton = page.getByRole('button', { name: 'Install Resource', exact: true })
    await expect(installButton).toBeVisible()
    await expect.poll(() => counts.actions).toBe(2)
    await installButton.screenshot({ path: testInfo.outputPath('remove-settled.png'), animations: 'disabled' })
    const afterActions = await captureFiles(rootPaths)
    assertFilesUnchanged(
      beforeNonUserMutation,
      await captureFiles(protectedOutsideUserMutation(bootstrap)),
      'confirmed removal changed organization, CWD, or unrelated materialized bytes',
    )
    if (materializedIndex >= 0 && afterActions[materializedIndex]?.exists) {
      throw new Error('confirmed removal left the Devin UserRoot target materialized')
    }
    expect(counts.plans).toBe(3)
    expect(counts.actions).toBe(2)
    expect(counts.coreActions).toBe(0)
    actions.push('Confirmed direct removal through the real Local API; the UserRoot target is absent and no core action endpoint was used')
    await saveActions(testInfo, actions)
  } finally {
    releaseActionResponse()
    if (!barrierReleased && bootstrapForCleanup != null) {
      await page.request.post(bootstrapForCleanup.resourceActionBarrierReleaseURL).catch(() => undefined)
    }
    await page.close().catch(() => undefined)
    try {
      await harness.stop()
    } finally {
      await saveActions(testInfo, actions)
    }
  }
})

// spec: session-interactions/guarded-submission
// spec: gui-shell/remove-and-exclude-keep-confirmation
test('built user GUI excludes and restores an optional organization pin without changing organization files', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the resource-action journey')
  }
  const harness = await startWork39Harness(testInfo, 'org-optional', false)
  const actions: string[] = []
  const counts = countRequests(page)
  let bootstrapForCleanup: Work39Bootstrap | null = null
  let barrierReleased = false
  try {
    const bootstrap = await work39Bootstrap(harness.bootstrap)
    bootstrapForCleanup = bootstrap
    const protectedPaths = allProtectedPaths(bootstrap)
    const baseline = await captureFiles(protectedPaths)
    const orgPaths = protectedOrganizationMutation(bootstrap)
    const orgBaseline = await captureFiles(orgPaths)
    await launchWorkspace(page, bootstrap, actions)
    const organizationRow = await selectReviewRow(page, actions)
    const organizationRowDOMID = await organizationRow.getAttribute('id')
    if (organizationRowDOMID == null) {
      throw new Error('Selected organization resource row had no DOM identity')
    }

    const cancelDialog = await openActionPlan(page, 'Exclude', actions)
    const preview = await cancelDialog.locator('pre').textContent()
    assertPreviewContainsNoLocalPaths(preview, [bootstrap.home!, bootstrap.repo!])
    await cancelAction(page, cancelDialog, actions)
    await expect(page.getByRole('button', { name: 'Exclude Resource', exact: true })).toBeFocused()
    assertFilesUnchanged(baseline, await captureFiles(protectedPaths), 'cancelled organization exclusion modified protected fixture files')
    actions.push('Cancelled the optional organization exclusion plan without changing user, organization, lock, materialized, or CWD bytes')

    const excludeDialog = await openActionPlan(page, 'Exclude', actions)
    const excludePreview = await excludeDialog.locator('pre').textContent()
    assertPreviewContainsNoLocalPaths(excludePreview, [bootstrap.home!, bootstrap.repo!])
    assertFilesUnchanged(baseline, await captureFiles(protectedPaths), 'opening exclusion modified protected fixture files before confirmation')
    const beforeExcludeCommit = await captureFiles(orgPaths)
    const userConfigBeforeExclude = await fileState(bootstrap.resourceActionUserConfig)
    const userLockBeforeExclude = await fileState(bootstrap.resourceActionUserLock)
    await excludeDialog.screenshot({ path: testInfo.outputPath('org-exclude-preview.png'), animations: 'disabled' })
    await confirmAction(page, excludeDialog, 'Exclude', actions)
    const heldFinal = await page.request.get(bootstrap.resourceActionBarrierWaitURL)
    expect(heldFinal.status()).toBe(204)
    await expect(excludeDialog).toBeHidden()
    await expect(mutationStatus(page)).toBeVisible()
    await expect(page.getByRole('button', { name: 'Exclude Resource', exact: true })).toBeDisabled()
    if (sameFileState(userConfigBeforeExclude, await fileState(bootstrap.resourceActionUserConfig))) {
      throw new Error('accepted exclusion did not update the user config before final delivery')
    }
    assertFilesUnchanged(beforeExcludeCommit, await captureFiles(orgPaths), 'organization or unrelated protected files changed on exclusion')
    if (!sameFileState(userLockBeforeExclude, await fileState(bootstrap.resourceActionUserLock))) {
      throw new Error('organization exclusion changed the user acquisition lock')
    }
    const release = await page.request.post(bootstrap.resourceActionBarrierReleaseURL)
    expect(release.status()).toBe(204)
    barrierReleased = true
    await expect(mutationStatus(page)).toBeHidden({ timeout: 60_000 })
    await selectRowByDOMID(page, organizationRowDOMID, actions)
    await expect(page.getByRole('button', { name: 'Restore Resource', exact: true })).toBeVisible()
    await expect.poll(async () => counts.actions).toBe(1)
    const afterExclude = await captureFiles(orgPaths)
    assertFilesUnchanged(beforeExcludeCommit, afterExclude, 'organization or unrelated protected files changed on exclusion')
    actions.push('Excluded the optional organization-owned resource using only the user-scope API mutation')
    await saveActions(testInfo, actions)

    const beforeRestoreCommit = await captureFiles(orgPaths)
    const beforeRestoreUserConfig = await fileState(bootstrap.resourceActionUserConfig)
    const beforeRestoreUserLock = await fileState(bootstrap.resourceActionUserLock)
    await trackOpenedDialogs(page)
    await page.getByRole('button', { name: 'Restore Resource', exact: true }).click()
    await expect.poll(async () => counts.actions).toBe(2)
    await expect(page.getByLabel('Notifications').getByText('Restored review')).toBeVisible({ timeout: 60_000 })
    expect(await stopDialogTracking(page)).toBe(false)
    await expect(page.getByRole('dialog', { name: 'Restore Resource', exact: true })).toHaveCount(0)
    if (sameFileState(beforeRestoreUserConfig, await fileState(bootstrap.resourceActionUserConfig))) {
      throw new Error('risk-free restore did not update the user config')
    }
    if (!sameFileState(beforeRestoreUserLock, await fileState(bootstrap.resourceActionUserLock))) {
      throw new Error('risk-free restore changed the user acquisition lock')
    }
    await selectReviewRow(page, actions)
    const excludeButton = page.getByRole('button', { name: 'Exclude Resource', exact: true })
    await expect(excludeButton).toBeVisible()
    assertFilesUnchanged(beforeRestoreCommit, await captureFiles(orgPaths), 'organization or unrelated protected files changed on restore')
    expect(counts.plans).toBe(3)
    expect(counts.actions).toBe(2)
    expect(counts.coreActions).toBe(0)
    await excludeButton.screenshot({ path: testInfo.outputPath('org-restored.png'), animations: 'disabled' })
    actions.push('Restored the optional organization resource; it remains enabled and the organization config/lock stayed byte-identical')
    await saveActions(testInfo, actions)
  } finally {
    if (!barrierReleased && bootstrapForCleanup != null) {
      await page.request.post(bootstrapForCleanup.resourceActionBarrierReleaseURL).catch(() => undefined)
    }
    await page.close().catch(() => undefined)
    try {
      await harness.stop()
    } finally {
      await saveActions(testInfo, actions)
    }
  }
})
