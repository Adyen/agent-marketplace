import { expect, test, type TestInfo } from '@playwright/test'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, isAbsolute, join, relative, sep } from 'node:path'
import { startHarness } from './harness'
import { distIndexExists, launchBuiltUserApp } from './app'
import { focusedDescription, tabTo } from './keyboard'

interface Bounds {
  x: number
  y: number
  width: number
  height: number
}

function expectHorizontallyContained(container: Bounds, panel: Bounds): void {
  expect(panel.x).toBeGreaterThanOrEqual(container.x - 1)
  expect(panel.x + panel.width).toBeLessThanOrEqual(container.x + container.width + 1)
}

async function saveActions(testInfo: TestInfo, actions: string[]): Promise<void> {
  const path = testInfo.outputPath('inspection.actions.json')
  await mkdir(dirname(path), { recursive: true })
  await writeFile(path, `${JSON.stringify({ actions }, null, 2)}\n`)
}

// spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
test('built GUI inspects an exact uninstalled Marketplace skill read-only', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the inspection journey')
  }
  const outputDir = dirname(testInfo.outputPath('inspection.actions.json'))
  if (!isAbsolute(outputDir)) {
    throw new Error('inspection evidence output directory must be absolute')
  }
  const actions: string[] = []
  const harnessLogDir = join(outputDir, 'harness')
  await mkdir(harnessLogDir, { recursive: true })
  const harness = await startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'work36-inspection',
    AM_LOCALAPI_RELEASE_VERSION: 'dev',
    AM_WORK28_TELEMETRY_EVIDENCE_DIR: harnessLogDir,
  })
  const { bootstrap } = harness
  try {
    await page.setViewportSize({ width: 1280, height: 800 })
    await launchBuiltUserApp(page, bootstrap)
    const list = page.getByRole('region', { name: 'Resources' })
    const remote = page.getByRole('option').filter({ hasText: 'grill' })
    const discoveryHealth = page.getByRole('region', { name: 'Marketplace discovery health' })
    await expect(list).toBeVisible({ timeout: 60_000 })
    await expect(remote).toBeVisible({ timeout: 30_000 })
    await expect(discoveryHealth).toContainText('fresh')
    if (bootstrap.marketplaceEndpoint == null) {
      throw new Error('Work36 verified Marketplace endpoint fixture missing')
    }

    let inspectionRequests = 0
    let mutationRequests = 0
    let discoveryRequests = 0
    page.on('request', (request) => {
      const pathname = new URL(request.url()).pathname
      if (pathname === '/api/inspection') {
        inspectionRequests++
      } else if (pathname === '/api/actions' || pathname === '/api/configuration/actions') {
        mutationRequests++
      } else if (pathname === '/api/discovery') {
        discoveryRequests++
      }
    })

    const search = page.getByRole('button', { name: 'Search', exact: true })
    const initialFocus = await focusedDescription(page)
    if (initialFocus !== 'button "Search"') {
      const navigationKey = initialFocus === '<none>' || initialFocus === 'body' ? 'Tab' : 'Shift+Tab'
      await tabTo(page, search, 'Search button', actions, undefined, navigationKey)
    }
    await tabTo(page, remote, 'Marketplace resource row', actions)
    await page.keyboard.press('Space')
    actions.push('Selected the uninstalled Marketplace resource by keyboard')
    await saveActions(testInfo, actions)

    const openResource = page.getByRole('button', { name: 'Open resource', exact: true })
    await expect(openResource).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath('inspection-ready.png'), fullPage: true, animations: 'disabled' })
    actions.push('Confirmed the cached uninstalled Marketplace skill is inspectable')
    await saveActions(testInfo, actions)
    await tabTo(page, openResource, 'Open resource button', actions)
    await page.keyboard.press('Enter')
    actions.push('Opened resource inspection by keyboard')
    await saveActions(testInfo, actions)

    const dialog = page.getByRole('dialog', { name: 'Resource inspection' })
    await expect(dialog).toBeVisible()
    const primary = dialog.getByRole('treeitem', { name: 'SKILL.md' })
    await expect(primary).toHaveAttribute('aria-selected', 'true')
    const preview = dialog.locator('pre')
    await expect(preview).toContainText('Grill inspection')
    await expect(preview).toContainText('<script>text</script>')
    expect(await dialog.locator('script, img').count()).toBe(0)
    const treePanel = dialog.getByRole('region', { name: 'Verified files' })
    const previewPanel = dialog.getByRole('region', { name: 'File preview' })
    const columns = dialog.locator('.resource-inspection__columns')
    const desktopDialogBox = await dialog.boundingBox()
    const desktopTreeBox = await treePanel.boundingBox()
    const desktopPreviewBox = await previewPanel.boundingBox()
    const desktopColumnsBox = await columns.boundingBox()
    if (desktopDialogBox == null || desktopTreeBox == null || desktopPreviewBox == null || desktopColumnsBox == null) {
      throw new Error('inspection desktop geometry unavailable')
    }
    expect(desktopDialogBox.width).toBeLessThanOrEqual(1280 - 16)
    expectHorizontallyContained(desktopDialogBox, desktopTreeBox)
    expectHorizontallyContained(desktopDialogBox, desktopPreviewBox)
    expectHorizontallyContained(desktopDialogBox, desktopColumnsBox)
    expect(desktopPreviewBox.width).toBeGreaterThanOrEqual(240)
    expect(desktopPreviewBox.x).toBeGreaterThanOrEqual(desktopTreeBox.x + desktopTreeBox.width - 1)
    expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
    actions.push('Desktop 1280x800: dialog and panels are contained; preview is at least 240px and columns do not overlap')
    await saveActions(testInfo, actions)

    await page.setViewportSize({ width: 640, height: 800 })
    const mobileDialogBox = await dialog.boundingBox()
    const mobileTreeBox = await treePanel.boundingBox()
    const mobilePreviewBox = await previewPanel.boundingBox()
    const mobileColumnsBox = await columns.boundingBox()
    if (mobileDialogBox == null || mobileTreeBox == null || mobilePreviewBox == null || mobileColumnsBox == null) {
      throw new Error('inspection mobile geometry unavailable')
    }
    expect(mobileDialogBox.width).toBeLessThanOrEqual(624)
    expectHorizontallyContained(mobileDialogBox, mobileTreeBox)
    expectHorizontallyContained(mobileDialogBox, mobilePreviewBox)
    expectHorizontallyContained(mobileDialogBox, mobileColumnsBox)
    expect(mobilePreviewBox.y).toBeGreaterThanOrEqual(mobileTreeBox.y + mobileTreeBox.height - 1)
    expect(Math.abs(mobilePreviewBox.x - mobileTreeBox.x)).toBeLessThanOrEqual(1)
    expect(await columns.evaluate((element) => getComputedStyle(element).gridTemplateColumns.trim().split(/\s+/).length)).toBe(1)
    expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
    actions.push('Mobile 640x800: inspection panels use one contained column')
    await saveActions(testInfo, actions)
    await page.setViewportSize({ width: 1280, height: 800 })
    await expect(dialog).toBeVisible()
    actions.push('Verified inspection panels fit desktop and mobile viewports')
    await saveActions(testInfo, actions)

    await page.screenshot({ path: testInfo.outputPath('inspection-primary.png'), fullPage: true, animations: 'disabled' })
    actions.push('Read the exact primary preview as inert text')
    await saveActions(testInfo, actions)

    const supportDirectory = dialog.getByRole('treeitem', { name: 'support', exact: true })
    await expect(primary).toBeFocused()
    await page.keyboard.press('ArrowUp')
    await expect(supportDirectory).toBeFocused()
    await page.keyboard.press('ArrowRight')
    await page.keyboard.press('ArrowRight')
    const image = dialog.getByRole('treeitem', { name: 'support/logo.png' })
    await expect(image).toBeFocused()
    await page.keyboard.press('ArrowLeft')
    await expect(supportDirectory).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(supportDirectory).toHaveAttribute('aria-expanded', 'false')
    await page.keyboard.press('Enter')
    await expect(supportDirectory).toHaveAttribute('aria-expanded', 'true')
    await page.keyboard.press('ArrowRight')
    await expect(image).toBeFocused()
    await page.keyboard.press('ArrowDown')
    const notes = dialog.getByRole('treeitem', { name: 'support/notes.md' })
    await expect(notes).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(dialog.getByRole('alert')).toContainText('Verified inspection is unavailable')
    await page.screenshot({ path: testInfo.outputPath('inspection-error.png'), fullPage: true, animations: 'disabled' })
    actions.push('Displayed the static error for a corrupted verified support response')
    await saveActions(testInfo, actions)

    const retry = dialog.getByRole('button', { name: 'Retry inspection', exact: true })
    await tabTo(page, retry, 'Retry inspection button', actions)
    await page.keyboard.press('Enter')
    await expect(preview).toContainText('Supporting inspection.')
    await expect(notes).toHaveAttribute('aria-selected', 'true')
    await expect(notes).toBeFocused()
    actions.push('Retried the exact selected member and received verified text')
    await page.screenshot({ path: testInfo.outputPath('inspection-support.png'), fullPage: true, animations: 'disabled' })
    await saveActions(testInfo, actions)

    await page.keyboard.press('ArrowUp')
    await expect(image).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(dialog.getByRole('status')).toContainText('Preview is not supported')
    await expect(dialog.locator('pre')).toHaveCount(0)
    await page.screenshot({ path: testInfo.outputPath('inspection-unsupported.png'), fullPage: true, animations: 'disabled' })
    actions.push('Viewed unsupported member metadata without rendering bytes')
    await saveActions(testInfo, actions)

    await page.keyboard.press('ArrowDown')
    await page.keyboard.press('ArrowDown')
    const svg = dialog.getByRole('treeitem', { name: 'support/unsafe.svg' })
    await expect(svg).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(dialog.getByRole('status')).toContainText('application/octet-stream')
    await expect(dialog.locator('pre')).toHaveCount(0)
    await page.screenshot({ path: testInfo.outputPath('inspection-unsupported-metadata.png'), fullPage: true, animations: 'disabled' })
    actions.push('Viewed unsupported metadata without requesting or rendering its body')
    await saveActions(testInfo, actions)

    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(openResource).toBeFocused()
    expect(discoveryRequests).toBe(0)
    expect(inspectionRequests).toBeGreaterThan(2)
    expect(mutationRequests).toBe(0)
    actions.push('Closed inspection with Escape; no install, sync, configuration, or discovery request was issued')
    await saveActions(testInfo, actions)
  } finally {
    await harness.stop()
    await saveActions(testInfo, actions)
  }
})

test('inspection harness stop propagates its protected-byte witness failure', async ({}, testInfo) => {
  const outputDir = dirname(testInfo.outputPath('inspection-harness-control.actions.json'))
  if (!isAbsolute(outputDir)) {
    throw new Error('inspection harness control output directory must be absolute')
  }
  const harnessLogDir = join(outputDir, 'harness-control')
  await mkdir(harnessLogDir, { recursive: true })
  const harness = await startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'work36-inspection',
    AM_LOCALAPI_RELEASE_VERSION: 'dev',
    AM_WORK28_TELEMETRY_EVIDENCE_DIR: harnessLogDir,
  })
  let expectedFailureObserved = false
  try {
    const home = harness.bootstrap.home
    if (home == null || !isAbsolute(home)) {
      throw new Error('inspection harness did not provide an absolute private home')
    }
    const relativeHome = relative(tmpdir(), home)
    if (relativeHome === '..' || relativeHome.startsWith(`..${sep}`) || isAbsolute(relativeHome)) {
      throw new Error('inspection harness home is outside the temporary test root')
    }
    const protectedPath = join(home, '.devin', 'skills', 'work36-protected', 'SKILL.md')
    if ((await readFile(protectedPath, 'utf8')) !== 'work36 protected materialized bytes\n') {
      throw new Error('inspection harness protected-file positive control was missing')
    }
    await writeFile(protectedPath, 'positive-control changed bytes\n')
    await expect(harness.stop()).rejects.toThrow('Local API harness failed')
    expectedFailureObserved = true
  } finally {
    if (!expectedFailureObserved) {
      await harness.stop().catch(() => undefined)
    }
  }
})
