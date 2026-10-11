import { test, expect, type Page, type TestInfo } from '@playwright/test'
import { mkdtemp, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { startHarness, type Harness } from './harness'
import { distIndexExists, serveBuiltApp } from './app'

const releaseBody = JSON.stringify({ tag_name: 'v2.0.0', draft: false, prerelease: false })

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
  expect(await response.json()).toEqual({ latestVersion })
}

async function startReleaseHarness(options: {
  version?: string
  status?: string
  body?: string
  org?: string
  userGuiFalse?: boolean
}): Promise<ReleaseFixture> {
  const dir = await mkdtemp(join(tmpdir(), 'am-release-'))
  const mark = join(dir, 'release-requests.log')
  const bodyPath = join(dir, 'latest.json')
  await writeFile(bodyPath, options.body ?? releaseBody)
  const harness = await startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'release',
    AM_LOCALAPI_RELEASE_VERSION: options.version ?? '1.0.0',
    AM_LOCALAPI_RELEASE_MARK: mark,
    AM_LOCALAPI_RELEASE_BODY: bodyPath,
    AM_LOCALAPI_RELEASE_STATUS: options.status ?? '200',
  })
  const { bootstrap } = harness
  const orgConfig = bootstrap.orgConfig!
  if (options.org != null) {
    await writeFile(orgConfig, options.org)
  }
  const userConfig = join(bootstrap.home!, '.agents', 'config.yaml')
  if (options.userGuiFalse === true) {
    const current = await readFile(userConfig, 'utf8')
    await writeFile(userConfig, `${current}gui:\n  version_check: false\n`)
  }
  return { harness, mark, orgConfig, userConfig }
}

async function launchBuiltUserApp(page: Page, url: string, credential: string) {
  await serveBuiltApp(page, url)
  await page.addInitScript(
    ([cred]) => {
      ;(window as unknown as Record<string, unknown>).__AM_GUI_BOOTSTRAP__ = { credential: cred }
    },
    [credential] as const,
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
// spec: config-layering/release-availability-checks-default-to-enabled
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
    await expect(page.getByText('skill/solo')).toBeVisible({ timeout: 60_000 })
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

// spec: gui-shell/organization-disables-release-checking
// spec: config-layering/organization-setting-controls-release-availability-checks
test('organization version_check false sends no release request and shows no notice', async ({ page }, testInfo) => {
  requireDist()
  const { harness, mark } = await startReleaseHarness({
    org: 'apiVersion: agent-marketplace/v4\ngui:\n  version_check: false\n',
  })
  try {
    const initialRelease = waitForReleaseResponse(page)
    await launchBuiltUserApp(page, harness.bootstrap.url, harness.bootstrap.credential)
    await expectReleaseResponse(initialRelease, '')
    await expect(page.getByText('skill/solo')).toBeVisible({ timeout: 60_000 })

    const refreshedRelease = waitForReleaseResponse(page)
    await page.getByRole('button', { name: 'Refresh' }).press('Enter')
    await expectReleaseResponse(refreshedRelease, '')
    await expect(page.getByText('Drift: ready')).toBeVisible({ timeout: 60_000 })
    expect(await readMark(mark)).toHaveLength(0)
    await expect(page.getByText('is available.')).not.toBeVisible()
    await expect(page.locator('.workspace__error')).toHaveCount(0)
    await page.screenshot({ path: artifactPath(testInfo, 'release-org-disabled-no-notice.png'), fullPage: true, animations: 'disabled' })
  } finally {
    await harness.stop()
  }
})

// spec: config-layering/lower-layer-gui-setting-is-ignored-with-a-warning
test('user-layer version_check false is ignored with a warning while checks stay enabled', async ({ page }) => {
  requireDist()
  const { harness, mark } = await startReleaseHarness({ userGuiFalse: true })
  try {
    const initialRelease = waitForReleaseResponse(page)
    await launchBuiltUserApp(page, harness.bootstrap.url, harness.bootstrap.credential)
    await expectReleaseResponse(initialRelease, 'v2.0.0')
    await expect(page.getByText('Agent Marketplace v2.0.0 is available.')).toBeVisible({ timeout: 60_000 })
    await expect(page.getByText('skill/solo')).toBeVisible({ timeout: 60_000 })
    expect(await readMark(mark)).toHaveLength(1)
    await expect(page.locator('.workspace__detail')).toContainText('version_check', { timeout: 60_000 })
  } finally {
    await harness.stop()
  }
})

// spec: gui-shell/version-check-fails
test('built GUI stays silent when the latest release matches or cannot be checked', async ({ page }) => {
  requireDist()
  const older = JSON.stringify({ tag_name: 'v0.9.0', draft: false, prerelease: false })
  const first = await startReleaseHarness({ body: older })
  try {
    const release = waitForReleaseResponse(page)
    await launchBuiltUserApp(page, first.harness.bootstrap.url, first.harness.bootstrap.credential)
    await expectReleaseResponse(release, '')
    await expect(page.getByText('skill/solo')).toBeVisible({ timeout: 60_000 })
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
    await expect(page.getByText('skill/solo')).toBeVisible({ timeout: 60_000 })
    expect(await readMark(second.mark)).toHaveLength(1)
    await expect(page.getByText('is available.')).not.toBeVisible()
    await expect(page.locator('.workspace__error')).toHaveCount(0)
    await expect(page.getByRole('link')).toHaveCount(0)
  } finally {
    await second.harness.stop()
  }
})
