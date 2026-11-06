import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, isAbsolute, join, relative, sep } from 'node:path'
import { startHarness } from './harness'
import { distIndexExists, launchBuiltUserApp } from './app'
import { focusedDescription, tabTo } from './keyboard'

async function startInstalledSkillHarness(testInfo: TestInfo, fixture: 'work37-installed' | 'work37-installed-missing') {
  const outputDir = dirname(testInfo.outputPath('installed-skill-files.actions.json'))
  if (!isAbsolute(outputDir)) {
    throw new Error('installed-skill-files evidence output directory must be absolute')
  }
  const harnessLogDir = join(outputDir, 'harness')
  await mkdir(harnessLogDir, { recursive: true })
  return startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: fixture,
    AM_LOCALAPI_RELEASE_VERSION: 'dev',
    AM_WORK28_TELEMETRY_EVIDENCE_DIR: harnessLogDir,
  })
}

async function saveActions(testInfo: TestInfo, actions: string[]): Promise<void> {
  await writeFile(testInfo.outputPath('installed-skill-files.actions.json'), `${JSON.stringify(actions, null, 2)}\n`)
}

async function openInstalledSkill(
  page: Page,
  bootstrap: Awaited<ReturnType<typeof startInstalledSkillHarness>>['bootstrap'],
  actions: string[],
): Promise<void> {
  const home = bootstrap.home
  if (home == null || !isAbsolute(home)) {
    throw new Error('installed-skill harness did not provide an absolute private home')
  }
  const relativeHome = relative(tmpdir(), home)
  if (relativeHome === '..' || relativeHome.startsWith(`..${sep}`) || isAbsolute(relativeHome)) {
    throw new Error('installed-skill harness home is outside the temporary test root')
  }
  await page.setViewportSize({ width: 1280, height: 800 })
  await launchBuiltUserApp(page, bootstrap)
  const resources = page.getByRole('region', { name: 'Resources' })
  const review = page.getByRole('option').filter({ hasText: 'review' })
  await expect(resources).toBeVisible({ timeout: 60_000 })
  await expect(review).toBeVisible({ timeout: 30_000 })
  const search = page.getByRole('button', { name: 'Search', exact: true })
  const initialFocus = await focusedDescription(page)
  if (initialFocus !== 'button "Search"') {
    const navigationKey = initialFocus === '<none>' || initialFocus === 'body' ? 'Tab' : 'Shift+Tab'
    await tabTo(page, search, 'Search button', actions, undefined, navigationKey)
  }
  await tabTo(page, review, 'Installed Marketplace skill row', actions)
  await page.keyboard.press('Space')
  actions.push('Selected the installed Marketplace skill row with Space')
  const openResource = page.getByRole('button', { name: 'Open resource', exact: true })
  await expect(openResource).toBeVisible()
  await tabTo(page, openResource, 'Open resource button', actions)
  await page.keyboard.press('Enter')
  await expect(page.getByRole('dialog', { name: 'Resource inspection' })).toBeVisible()
  actions.push('Opened Resource inspection with Enter')
}

// spec: session-navigation/installed-skill-definition-and-supporting-files-remain-browsable
test('built GUI browses installed Marketplace skill definition and declared support files read-only', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the installed-files journey')
  }
  const harness = await startInstalledSkillHarness(testInfo, 'work37-installed')
  const actions: string[] = []
  try {
    let installedFileRequests = 0
    let sdkInspectionRequests = 0
    let mutationRequests = 0
    let discoveryRequests = 0
    page.on('request', (request) => {
      const pathname = new URL(request.url()).pathname
      if (pathname === '/api/installed-skill-files') {
        installedFileRequests++
      } else if (pathname === '/api/inspection') {
        sdkInspectionRequests++
      } else if (pathname === '/api/actions' || pathname === '/api/configuration/actions') {
        mutationRequests++
      } else if (pathname === '/api/discovery') {
        discoveryRequests++
      }
    })
    await openInstalledSkill(page, harness.bootstrap, actions)
    await saveActions(testInfo, actions)

    const dialog = page.getByRole('dialog', { name: 'Resource inspection' })
    const primary = dialog.getByRole('treeitem', { name: 'SKILL.md' })
    await expect(primary).toHaveAttribute('aria-selected', 'true')
    await expect(primary).toBeFocused()
    const preview = dialog.locator('pre')
    await expect(preview).toContainText('Installed review')
    expect(await dialog.locator('script, img').count()).toBe(0)
    actions.push('Received the canonical SKILL.md preview as inert text')
    await saveActions(testInfo, actions)
    await page.screenshot({ path: testInfo.outputPath('installed-primary.png'), fullPage: true, animations: 'disabled' })

    await page.keyboard.press('ArrowUp')
    const notes = dialog.getByRole('treeitem', { name: 'notes.md' })
    await expect(notes).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(preview).toContainText('Supporting review')
    actions.push('Read notes.md through the keyboard file tree')
    await saveActions(testInfo, actions)
    await page.screenshot({ path: testInfo.outputPath('installed-notes.png'), fullPage: true, animations: 'disabled' })

    await page.keyboard.press('ArrowUp')
    const scripts = dialog.getByRole('treeitem', { name: 'scripts' })
    await expect(scripts).toBeFocused()
    await page.keyboard.press('ArrowRight')
    await page.keyboard.press('ArrowRight')
    const helper = dialog.getByRole('treeitem', { name: 'scripts/helper.sh' })
    await expect(helper).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(preview).toContainText('printf "review"')
    expect(await dialog.locator('script, img').count()).toBe(0)
    actions.push('Read scripts/helper.sh as inert preformatted text')
    await saveActions(testInfo, actions)
    await page.screenshot({ path: testInfo.outputPath('installed-script.png'), fullPage: true, animations: 'disabled' })

    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(page.getByRole('button', { name: 'Open resource', exact: true })).toBeFocused()
    expect(installedFileRequests).toBe(3)
    expect(sdkInspectionRequests).toBe(0)
    expect(mutationRequests).toBe(0)
    expect(discoveryRequests).toBe(0)
    actions.push('Closed with Escape; verified 3 installed-file requests and no SDK, mutation, or discovery requests')
    await saveActions(testInfo, actions)
  } finally {
    try {
      await harness.stop()
    } finally {
      await saveActions(testInfo, actions)
    }
  }
})

test('missing installed support content remains a static retryable error', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the GUI build before the installed-files journey')
  }
  const harness = await startInstalledSkillHarness(testInfo, 'work37-installed-missing')
  const actions: string[] = []
  try {
    let installedFileRequests = 0
    let sdkInspectionRequests = 0
    let mutationRequests = 0
    let discoveryRequests = 0
    page.on('request', (request) => {
      const pathname = new URL(request.url()).pathname
      if (pathname === '/api/installed-skill-files') {
        installedFileRequests++
      } else if (pathname === '/api/inspection') {
        sdkInspectionRequests++
      } else if (pathname === '/api/actions' || pathname === '/api/configuration/actions') {
        mutationRequests++
      } else if (pathname === '/api/discovery') {
        discoveryRequests++
      }
    })
    await openInstalledSkill(page, harness.bootstrap, actions)
    const dialog = page.getByRole('dialog', { name: 'Resource inspection' })
    const alert = dialog.getByRole('alert')
    await expect(alert).toHaveText('Resource files are unavailable; refresh this resource and retry.')
    actions.push('Received the static error for missing staged support content')
    await saveActions(testInfo, actions)
    await page.screenshot({ path: testInfo.outputPath('installed-error.png'), fullPage: true, animations: 'disabled' })
    const retry = dialog.getByRole('button', { name: 'Retry inspection', exact: true })
    await tabTo(page, retry, 'Retry inspection button', actions)
    const retryResponse = page.waitForResponse((response) =>
      new URL(response.url()).pathname === '/api/installed-skill-files' && response.status() === 502,
    )
    await page.keyboard.press('Enter')
    const response = await retryResponse
    expect(response.status()).toBe(502)
    await expect(dialog.getByRole('status')).toHaveCount(0)
    await expect(alert).toHaveText('Resource files are unavailable; refresh this resource and retry.')
    expect(installedFileRequests).toBe(2)
    actions.push('Confirmed the retry completed with a second 502 and the static error settled')
    await saveActions(testInfo, actions)
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(page.getByRole('button', { name: 'Open resource', exact: true })).toBeFocused()
    expect(installedFileRequests).toBe(2)
    expect(sdkInspectionRequests).toBe(0)
    expect(mutationRequests).toBe(0)
    expect(discoveryRequests).toBe(0)
    actions.push('Closed with Escape; focus recovered and no writes, SDK inspection, or discovery requests occurred')
    await saveActions(testInfo, actions)
  } finally {
    try {
      await harness.stop()
    } finally {
      await saveActions(testInfo, actions)
    }
  }
})
