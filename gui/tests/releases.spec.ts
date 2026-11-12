import { test, expect, type Page, type TestInfo } from '@playwright/test'
import { mkdtemp, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { startHarness, type Harness } from './harness'
import { distIndexExists, serveBuiltApp } from './app'

function releaseBody(tag = 'v2.0.0') {
  return JSON.stringify({
    tag_name: tag,
    draft: false,
    prerelease: false,
    published_at: '2026-10-30T00:00:00Z',
    html_url: `https://github.com/Adyen/agent-marketplace/releases/tag/${tag}`,
    assets: [],
  })
}

function packagedReleaseNotice() {
  return {
    latestVersion: 'v2.0.0',
    releasePageURL: 'https://github.com/Adyen/agent-marketplace/releases/tag/v2.0.0',
    assetURL: 'https://github.com/Adyen/agent-marketplace/releases/download/v2.0.0/gui.zip',
    checksumsURL: 'https://github.com/Adyen/agent-marketplace/releases/download/v2.0.0/SHA256SUMS',
    dmgURL: 'https://github.com/Adyen/agent-marketplace/releases/download/v2.0.0/gui.dmg',
  }
}

function updateStatus(phase: string, overrides: Record<string, unknown> = {}) {
  return {
    phase,
    progress: 0,
    error: '',
    browserURL: '',
    releasePageURL: '',
    savedPath: '',
    moveToApplications: false,
    restartRequired: false,
    ...overrides,
  }
}

interface ReleaseFixture {
  harness: Harness
  mark: string
  orgConfig: string
  userConfig: string
}

async function readMark(mark: string): Promise<string[]> {
  try {
    const text = await readFile(mark, 'utf8')
    return text.split('\n').filter((line) => line !== '')
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') return []
    throw error
  }
}

function waitForReleaseResponse(page: Page) {
  return page.waitForResponse((response) => {
    const request = response.request()
    return request.method() === 'POST' && new URL(response.url()).pathname === '/api/release'
  })
}

async function expectReleaseResponse(responsePromise: ReturnType<typeof waitForReleaseResponse>, latestVersion: string) {
  const response = await responsePromise
  expect(response.status()).toBe(200)
  expect(await response.json()).toEqual({
    latestVersion,
    releasePageURL: latestVersion ? `https://github.com/Adyen/agent-marketplace/releases/tag/${latestVersion}` : '',
    assetURL: '',
    checksumsURL: '',
    dmgURL: '',
  })
}

async function startReleaseHarness(options: {
  version?: string
  status?: string
  body?: string
  org?: string
  userUpdateCheckFalse?: boolean
}): Promise<ReleaseFixture> {
  const dir = await mkdtemp(join(tmpdir(), 'am-release-'))
  const mark = join(dir, 'release-requests.log')
  const bodyPath = join(dir, 'latest.json')
  await writeFile(bodyPath, options.body ?? releaseBody())
  const harness = await startHarness('user', {
    AGENT_MARKETPLACE_NO_UPDATE_CHECK: '',
    AM_LOCALAPI_USER_FIXTURE: 'release',
    AM_LOCALAPI_RELEASE_VERSION: options.version ?? 'v1.0.0',
    AM_LOCALAPI_RELEASE_MARK: mark,
    AM_LOCALAPI_RELEASE_BODY: bodyPath,
    AM_LOCALAPI_RELEASE_STATUS: options.status ?? '200',
    CI: '',
  })
  const { bootstrap } = harness
  const orgConfig = bootstrap.orgConfig!
  if (options.org != null) {
    await writeFile(orgConfig, options.org)
  }
  const userConfig = join(bootstrap.home!, '.agents', 'config.yaml')
  if (options.userUpdateCheckFalse === true) {
    const current = await readFile(userConfig, 'utf8')
    await writeFile(userConfig, `${current}update_check:\n  enabled: false\n`)
  }
  return { harness, mark, orgConfig, userConfig }
}

async function launchBuiltUserApp(page: Page, url: string, credential: string, packageFormat = '', serve = true) {
  if (serve) {
    await serveBuiltApp(page, url)
  }
  await page.addInitScript(
    ([cred, format]) => {
      ;(window as unknown as Record<string, unknown>).__AM_GUI_BOOTSTRAP__ = { credential: cred, packageFormat: format }
    },
    [credential, packageFormat] as const,
  )
  await page.goto(url, { waitUntil: 'domcontentloaded' })
}

function requireDist() {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `pnpm run build` in gui/ first')
  }
}

function artifactPath(testInfo: TestInfo, name: string): string {
  const directory = process.env.AM_WORK27_RELEASE_EVIDENCE_DIR
  return directory == null ? testInfo.outputPath(name) : join(directory, name)
}

// spec: gui-shell/new-stable-release-is-available
// spec: gui-shell/unpackaged-build-links-to-the-release-page
test('built GUI shows a newer stable release notice once per launch', async ({ page }, testInfo) => {
  requireDist()
  const { harness, mark, orgConfig, userConfig } = await startReleaseHarness({})
  const orgBefore = await readFile(orgConfig)
  const userBefore = await readFile(userConfig)
  try {
    const initialRelease = waitForReleaseResponse(page)
    await launchBuiltUserApp(page, harness.bootstrap.url, harness.bootstrap.credential)
    await expectReleaseResponse(initialRelease, 'v2.0.0')
    await expect(page.getByText('Agent Marketplace v2.0.0 is available.')).toBeVisible({ timeout: 60_000 })
    await expect(page.getByRole('link', { name: 'View release' })).toHaveAttribute(
      'href',
      'https://github.com/Adyen/agent-marketplace/releases/tag/v2.0.0',
    )
    await expect(page.getByRole('option').filter({ hasText: 'solo' }).first()).toBeVisible({ timeout: 60_000 })
    await page.screenshot({ path: artifactPath(testInfo, 'release-notice-visible.png'), fullPage: true, animations: 'disabled' })

    const refreshedRelease = waitForReleaseResponse(page)
    await page.getByRole('button', { name: 'Refresh' }).press('Enter')
    await expectReleaseResponse(refreshedRelease, 'v2.0.0')
    await expect(page.getByText('Agent Marketplace v2.0.0 is available.')).toBeVisible()
    const requests = await readMark(mark)
    expect(requests).toHaveLength(1)
    expect(requests[0]).toBe('GET https://api.github.com/repos/Adyen/agent-marketplace/releases/latest')

    expect((await readFile(orgConfig)).equals(orgBefore)).toBe(true)
    expect((await readFile(userConfig)).equals(userBefore)).toBe(true)
  } finally {
    await harness.stop()
  }
})

// spec: gui-shell/pending-work-requires-confirmation-before-the-update
test('built GUI confirms a macOS update only after pending work is accepted', async ({ page }) => {
  requireDist()
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'pending-sync' })
  const actions: string[] = []
  await serveBuiltApp(page, harness.bootstrap.url)
  await page.route('**/api/release', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(packagedReleaseNotice()),
    }),
  )
  await page.route('**/api/update', async (route) => {
    const action = (JSON.parse(route.request().postData() ?? '{}') as { action: string }).action
    actions.push(action)
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(updateStatus(
        action === 'start' ? 'downloading' : action === 'status' ? 'ready' : action === 'cancel' ? 'cancelled' : 'swapping',
        { progress: action === 'start' ? 34 : 100 },
      )),
    })
  })

  try {
    await launchBuiltUserApp(page, harness.bootstrap.url, harness.bootstrap.credential, 'macos-app', false)
    await expect(page.getByRole('option').filter({ hasText: 'solo' }).first()).toBeVisible({ timeout: 60_000 })
    const update = page.getByRole('button', { name: 'Update now' })
    await expect(update).toBeVisible()
    expect(actions).toEqual([])

    await update.click()
    const progress = page.getByRole('progressbar', { name: 'Update progress' })
    await expect(progress).toHaveAttribute('value', '34')
    const quitDialog = page.getByRole('dialog', { name: 'Quit Agent Marketplace' })
    await expect(quitDialog).toBeVisible()
    expect(actions).toEqual(['start', 'status'])

    await quitDialog.getByRole('button', { name: 'Cancel quit' }).click()
    await expect.poll(() => actions).toEqual(['start', 'status', 'cancel'])
    await update.click()
    await expect(quitDialog).toBeVisible()
    await quitDialog.getByRole('button', { name: 'Quit without syncing' }).click()
    await expect.poll(() => actions).toEqual(['start', 'status', 'cancel', 'start', 'status', 'confirm'])
  } finally {
    await harness.stop()
  }
})

// spec: gui-shell/user-updates-the-app-in-place
test('built GUI confirms and completes an in-place update when no work is pending', async ({ page }) => {
  requireDist()
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'release' })
  const actions: string[] = []
  await serveBuiltApp(page, harness.bootstrap.url)
  await page.route('**/api/release', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(packagedReleaseNotice()) }),
  )
  await page.route('**/api/update', async (route) => {
    const action = (JSON.parse(route.request().postData() ?? '{}') as { action: string }).action
    actions.push(action)
    const body = action === 'start'
      ? updateStatus('ready', { progress: 100 })
      : updateStatus('complete', { progress: 100 })
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
  })
  await page.addInitScript(() => {
    const testWindow = window as unknown as {
      __quitCalls: number
      go: { main: { App: { Quit: () => void } } }
    }
    testWindow.__quitCalls = 0
    testWindow.go = { main: { App: { Quit: () => { testWindow.__quitCalls++ } } } }
  })

  try {
    await launchBuiltUserApp(page, harness.bootstrap.url, harness.bootstrap.credential, 'macos-app', false)
    await expect(page.getByRole('option').filter({ hasText: 'solo' }).first()).toBeVisible({ timeout: 60_000 })
    await page.getByRole('button', { name: 'Sync', exact: true }).click()
    await expect(page.getByText('sync complete')).toBeVisible({ timeout: 60_000 })
    await page.getByRole('button', { name: 'Update now' }).click()
    await expect(page.getByText('Update complete. Restarting Agent Marketplace.')).toBeVisible({ timeout: 60_000 })
    expect(actions).toEqual(['start', 'confirm'])
    await expect.poll(() => page.evaluate(() => (window as unknown as { __quitCalls: number }).__quitCalls)).toBe(1)
    await expect(page.getByRole('dialog', { name: 'Quit Agent Marketplace' })).toHaveCount(0)
  } finally {
    await harness.stop()
  }
})

// spec: gui-shell/corrupted-update-download-is-rejected
// spec: gui-shell/ineligible-location-falls-back-to-a-browser-download
test('built GUI offers retry and the browser download when in-place update fails', async ({ page }) => {
  requireDist()
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'release' })
  let starts = 0
  await serveBuiltApp(page, harness.bootstrap.url)
  await page.route('**/api/release', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(packagedReleaseNotice()) }),
  )
  await page.route('**/api/update', async (route) => {
    const action = (JSON.parse(route.request().postData() ?? '{}') as { action: string }).action
    if (action === 'start') starts++
    const body = action === 'start' && starts === 1
      ? updateStatus('failed', { error: 'verification failed' })
      : updateStatus('fallback', {
          browserURL: packagedReleaseNotice().dmgURL,
          releasePageURL: packagedReleaseNotice().releasePageURL,
          moveToApplications: true,
        })
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
  })

  try {
    await launchBuiltUserApp(page, harness.bootstrap.url, harness.bootstrap.credential, 'macos-app', false)
    const update = page.getByRole('button', { name: 'Update now' })
    await expect(update).toBeVisible({ timeout: 60_000 })
    await update.click()
    await expect(page.getByRole('alert')).toContainText('The update could not be completed.')
    await page.getByRole('button', { name: 'Try update again' }).click()
    await expect(page.getByRole('link', { name: 'Download disk image' })).toHaveAttribute('href', packagedReleaseNotice().dmgURL)
    await expect(page.getByText('Move the app to Applications to enable updates.')).toBeVisible()
  } finally {
    await harness.stop()
  }
})

// spec: gui-shell/user-hands-the-verified-package-to-the-software-installer
test('built Linux GUI tells the user to restart after package handoff', async ({ page }) => {
  requireDist()
  const harness = await startHarness('user', { AM_LOCALAPI_USER_FIXTURE: 'release' })
  await serveBuiltApp(page, harness.bootstrap.url)
  await page.route('**/api/release', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(packagedReleaseNotice()) }),
  )
  await page.route('**/api/update', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(updateStatus('complete', {
        progress: 100,
        savedPath: '/home/user/Downloads/agent-marketplace-gui.rpm',
        restartRequired: true,
      })),
    }),
  )

  try {
    await launchBuiltUserApp(page, harness.bootstrap.url, harness.bootstrap.credential, 'rpm', false)
    await page.getByRole('button', { name: 'Update now' }).click()
    await expect(page.getByText('Install the package, then restart Agent Marketplace.')).toBeVisible({ timeout: 60_000 })
  } finally {
    await harness.stop()
  }
})

// spec: gui-shell/organization-disables-release-checking
test('organization update_check false sends no release request and shows no notice', async ({ page }, testInfo) => {
  requireDist()
  const { harness, mark } = await startReleaseHarness({
    org: 'apiVersion: agent-marketplace/v4\nupdate_check:\n  enabled: false\n',
  })
  try {
    const initialRelease = waitForReleaseResponse(page)
    await launchBuiltUserApp(page, harness.bootstrap.url, harness.bootstrap.credential)
    await expectReleaseResponse(initialRelease, '')
    await expect(page.getByRole('option').filter({ hasText: 'solo' }).first()).toBeVisible({ timeout: 60_000 })

    const refreshedRelease = waitForReleaseResponse(page)
    await page.getByRole('button', { name: 'Refresh' }).press('Enter')
    await expectReleaseResponse(refreshedRelease, '')
    await page.getByRole('button', { name: 'Show diagnostics' }).click()
    await expect(page.getByText('Drift: ready')).toBeVisible({ timeout: 60_000 })
    await page.getByRole('button', { name: 'Show all groups' }).click()
    expect(await readMark(mark)).toHaveLength(0)
    await expect(page.getByText('is available.')).not.toBeVisible()
    await expect(page.locator('.workspace__error')).toHaveCount(0)
    await page.screenshot({ path: artifactPath(testInfo, 'release-org-disabled-no-notice.png'), fullPage: true, animations: 'disabled' })
  } finally {
    await harness.stop()
  }
})

// spec: gui-shell/update-checking-is-suppressed
test('user update_check false sends no release request and shows no notice', async ({ page }) => {
  requireDist()
  const { harness, mark } = await startReleaseHarness({ userUpdateCheckFalse: true })
  try {
    const initialRelease = waitForReleaseResponse(page)
    await launchBuiltUserApp(page, harness.bootstrap.url, harness.bootstrap.credential)
    await expectReleaseResponse(initialRelease, '')
    await expect(page.getByRole('option').filter({ hasText: 'solo' }).first()).toBeVisible({ timeout: 60_000 })
    expect(await readMark(mark)).toHaveLength(0)
    await expect(page.getByText('is available.')).not.toBeVisible()
  } finally {
    await harness.stop()
  }
})

// spec: gui-shell/version-check-fails
test('built GUI stays silent when the latest release matches or cannot be checked', async ({ page }) => {
  requireDist()
  const older = releaseBody('v0.9.0')
  const first = await startReleaseHarness({ body: older })
  try {
    const release = waitForReleaseResponse(page)
    await launchBuiltUserApp(page, first.harness.bootstrap.url, first.harness.bootstrap.credential)
    await expectReleaseResponse(release, '')
    await expect(page.getByRole('option').filter({ hasText: 'solo' }).first()).toBeVisible({ timeout: 60_000 })
    expect(await readMark(first.mark)).toHaveLength(1)
    await expect(page.getByText('is available.')).not.toBeVisible()
    await expect(page.locator('.workspace__error')).toHaveCount(0)
  } finally {
    await first.harness.stop()
  }

  const second = await startReleaseHarness({ status: '500' })
  try {
    const release = waitForReleaseResponse(page)
    await launchBuiltUserApp(page, second.harness.bootstrap.url, second.harness.bootstrap.credential)
    await expectReleaseResponse(release, '')
    await expect(page.getByRole('option').filter({ hasText: 'solo' }).first()).toBeVisible({ timeout: 60_000 })
    expect(await readMark(second.mark)).toHaveLength(1)
    await expect(page.getByText('is available.')).not.toBeVisible()
    await expect(page.locator('.workspace__error')).toHaveCount(0)
    await expect(page.getByRole('link')).toHaveCount(0)
  } finally {
    await second.harness.stop()
  }
})
