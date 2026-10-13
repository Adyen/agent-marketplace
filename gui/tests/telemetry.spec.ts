import { test, expect, type Locator, type Page, type TestInfo } from '@playwright/test'
import { existsSync } from 'node:fs'
import { lstat, mkdir, readFile, realpath, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, isAbsolute, join, relative, sep } from 'node:path'
import { startHarness, type Bootstrap } from './harness'
import { distIndexExists, serveBuiltApp } from './app'
import { tabTo } from './keyboard'

interface TelemetrySpan {
  name: string
  attributes: Record<string, string>
}

interface TelemetryMetric {
  name: string
  attributes: Record<string, string>
  value?: number
  count?: number
}

interface TelemetryCapture {
  spans: TelemetrySpan[]
  metrics: TelemetryMetric[]
  requests: { traces: number; metrics: number }
}

function artifactPath(testInfo: TestInfo, name: string): string {
  const directory = process.env.AM_WORK28_TELEMETRY_EVIDENCE_DIR
  return directory == null ? testInfo.outputPath(name) : join(directory, name)
}

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

async function startTelemetryHarness(testInfo: TestInfo, mode: 'detailed' | 'basic' | 'off') {
  const capturePath = artifactPath(testInfo, `telemetry-${mode}.json`)
  await mkdir(dirname(capturePath), { recursive: true })
  const harness = await startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'navigation',
    AM_LOCALAPI_TELEMETRY_MODE: mode,
    AM_LOCALAPI_TELEMETRY_CAPTURE: capturePath,
    AM_LOCALAPI_RELEASE_MARK: '',
  })
  return { ...harness, capturePath }
}

async function readCapture(path: string): Promise<TelemetryCapture> {
  return JSON.parse(await readFile(path, 'utf8')) as TelemetryCapture
}

function observeTelemetryRequests(page: Page): { count: () => number } {
  let count = 0
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/telemetry') {
      count++
    }
  })
  return { count: () => count }
}

interface Work40TelemetryBootstrap extends Bootstrap {
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
}

interface Work40FileState {
  path: string
  exists: boolean
  mode: number | null
  bytes: Buffer | null
}

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

async function work40TelemetryBootstrap(value: Bootstrap): Promise<Work40TelemetryBootstrap> {
  const bootstrap = value as Work40TelemetryBootstrap
  const paths: [string, string | undefined][] = [
    ['home', bootstrap.home],
    ['repo', bootstrap.repo],
    ['user config', bootstrap.work40UserConfig],
    ['user fragment', bootstrap.work40UserFragment],
    ['user lock', bootstrap.work40UserLock],
    ['organization config', bootstrap.work40OrgConfig],
    ['organization lock', bootstrap.work40OrgLock],
    ['registry', bootstrap.work40Directory],
    ['review source', bootstrap.work40ReviewSource],
    ['next source', bootstrap.work40NextSource],
    ['review materialization', bootstrap.work40ReviewMaterialized],
    ['next materialization', bootstrap.work40NextMaterialized],
    ['unrelated materialization', bootstrap.work40UnrelatedMaterialized],
  ]
  const invalidPaths = await Promise.all(paths.map(async ([name, path]) =>
    path == null || !isAbsolute(path) || !await isWithinTemp(path) ? name : null,
  ))
  if (bootstrap.work40Mode !== 'root-owned' || invalidPaths.some((path) => path != null)) {
    throw new Error('Work40 telemetry fixture is not an isolated root-owned directory case')
  }
  const endpoint = new URL(bootstrap.url)
  if (endpoint.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(endpoint.hostname)) {
    throw new Error('Work40 telemetry harness must use a loopback endpoint')
  }
  return bootstrap
}

async function startWork40TelemetryHarness(testInfo: TestInfo) {
  const capturePath = testInfo.outputPath('toggle-cancellation.json')
  const outputDir = dirname(capturePath)
  const harnessLogDir = join(outputDir, 'harness')
  await mkdir(harnessLogDir, { recursive: true })
  const harness = await startHarness('user', {
    AM_LOCALAPI_USER_FIXTURE: 'work40-resource-actions',
    AM_LOCALAPI_USER_INITIAL_MODE: 'root-owned',
    AM_LOCALAPI_TELEMETRY_MODE: 'detailed',
    AM_LOCALAPI_TELEMETRY_CAPTURE: capturePath,
    AM_LOCALAPI_RELEASE_MARK: '',
    AM_LOCALAPI_RELEASE_VERSION: 'dev',
    AM_WORK28_TELEMETRY_EVIDENCE_DIR: harnessLogDir,
  })
  return { ...harness, capturePath }
}

function work40ProtectedPaths(bootstrap: Work40TelemetryBootstrap): string[] {
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

async function captureWork40File(path: string): Promise<Work40FileState> {
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

async function captureWork40Files(paths: string[]): Promise<Work40FileState[]> {
  return Promise.all(paths.map(captureWork40File))
}

function assertWork40FilesUnchanged(before: Work40FileState[], after: Work40FileState[]): void {
  if (before.length !== after.length || before.some((left, index) => {
    const right = after[index]!
    const sameBytes = left.bytes == null ? right.bytes == null : right.bytes != null && left.bytes.equals(right.bytes)
    return left.path !== right.path || left.exists !== right.exists || left.mode !== right.mode || !sameBytes
  })) {
    throw new Error('Work40 protected file bytes or modes changed during cancellation')
  }
}

function assertWork40PreviewHasNoLocalPaths(preview: string | null, bootstrap: Work40TelemetryBootstrap): void {
  if (preview != null && [bootstrap.home!, bootstrap.repo!, bootstrap.work40Directory].some((path) => preview.includes(path))) {
    throw new Error('Work40 static preview exposed a local path')
  }
}

function observeWork40ActionRequests(page: Page): { plans: () => number; resourceActions: () => number; coreActions: () => number } {
  let plans = 0
  let resourceActions = 0
  let coreActions = 0
  page.on('request', (request) => {
    if (request.method() !== 'POST') {
      return
    }
    switch (new URL(request.url()).pathname) {
      case '/api/resource-plan':
        plans++
        break
      case '/api/resource-actions':
        resourceActions++
        break
      case '/api/actions':
        coreActions++
        break
    }
  })
  return { plans: () => plans, resourceActions: () => resourceActions, coreActions: () => coreActions }
}

async function openWork40InstallPlan(page: Page, actions: string[]): Promise<Locator> {
  const installButton = page.getByRole('button', { name: 'Install Resource', exact: true })
  await expect(installButton).toBeEnabled()
  await tabTo(page, installButton, 'Install Resource', actions)
  const response = page.waitForResponse((candidate) => candidate.request().method() === 'POST' && new URL(candidate.url()).pathname === '/api/resource-plan')
  await page.keyboard.press('Enter')
  expect((await response).status()).toBe(200)
  const dialog = page.getByRole('dialog', { name: 'Install Resource' })
  await expect(dialog).toBeVisible()
  return dialog
}

async function saveWork41Actions(testInfo: TestInfo, actions: string[]): Promise<void> {
  const path = testInfo.outputPath('toggle-cancellation.actions.json')
  await mkdir(dirname(path), { recursive: true })
  await writeFile(path, JSON.stringify({ actions }, null, 2))
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
test('built GUI sends canonical detailed browse, sync, and sync-choice cancellation events', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
  }

  const harness = await startTelemetryHarness(testInfo, 'detailed')
  const { bootstrap, capturePath } = harness
  const events = observeTelemetryRequests(page)

  try {
    await launchBuiltUserApp(page, bootstrap)
    await expect(page.getByRole('option').filter({ hasText: 'skill/skill-000' }).first()).toBeVisible({ timeout: 60_000 })

    const quit = page.getByRole('button', { name: 'Quit', exact: true })
    await quit.press('Enter')
    const dialog = page.getByRole('dialog', { name: 'Quit Agent Marketplace' })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Sync and quit' })).toBeVisible()
    const cancelResponse = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/telemetry')
    await dialog.getByRole('button', { name: 'Cancel quit' }).press('Enter')
    expect((await cancelResponse).status()).toBe(204)
    await expect(dialog).not.toBeVisible()

    const resource = page.getByRole('option').filter({ hasText: 'skill/skill-001' })
    const browseResponse = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/telemetry')
    await resource.press('Enter')
    expect((await browseResponse).status()).toBe(204)
    await expect(page.getByRole('complementary', { name: 'Detail' }).getByRole('heading').first()).toContainText('skill/skill-001')

    const actionResponse = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/actions')
    await page.getByRole('button', { name: 'Sync', exact: true }).press('Enter')
    expect((await actionResponse).status()).toBe(202)
    await expect(page.getByText('sync complete')).toBeVisible({ timeout: 60_000 })
    expect(events.count()).toBe(2)
    await page.screenshot({ path: artifactPath(testInfo, 'telemetry-detailed-browse-sync.png'), fullPage: true, animations: 'disabled' })
  } finally {
    await harness.stop()
  }

  const capture = await readCapture(capturePath)
  const command = capture.spans.find((span) => span.name === 'agent_marketplace.command')
  expect(command?.attributes).toEqual({ 'command.name': 'gui', 'command.surface': 'gui' })
  expect(capture.requests.traces).toBeGreaterThan(0)
  expect(capture.metrics).toHaveLength(2)
  expect(capture.metrics.map((metric) => metric.name).sort()).toEqual([
    'agent_marketplace.command.duration_ms',
    'agent_marketplace.command.invocations',
  ])
  for (const metric of capture.metrics) {
    expect(metric.attributes).toEqual({
      'command.name': 'gui',
      harnesses: 'devin',
      managed: 'false',
      'outcome.code': 'success',
    })
  }
  const actions = capture.spans.filter((span) => span.name === 'agent_marketplace.gui.action')
  expect(actions).toHaveLength(3)
  expect(actions.map((span) => span.attributes.action).sort()).toEqual(['browse', 'modal_outcome', 'sync'])
  expect(actions.find((span) => span.attributes.action === 'browse')?.attributes).toEqual({
    action: 'browse',
    'command.surface': 'gui',
    view: 'detail',
    'resource.kind': 'skill',
  })
  expect(actions.find((span) => span.attributes.action === 'modal_outcome')?.attributes).toEqual({
    action: 'modal_outcome',
    'command.surface': 'gui',
    modal: 'sync+quit',
    outcome: 'cancelled',
  })
  expect(actions.find((span) => span.attributes.action === 'sync')?.attributes).toEqual({
    action: 'sync',
    'command.surface': 'gui',
  })
  expect(JSON.stringify(actions)).not.toContain('rowId')
  expect(JSON.stringify(actions)).not.toContain('solo')
})

for (const mode of ['basic', 'off'] as const) {
  // spec: gui-shell/gui-actions-use-existing-telemetry-policy
  test(`built GUI completes browse telemetry requests with ${mode} policy`, async ({ page }, testInfo) => {
    if (!distIndexExists()) {
      throw new Error('built GUI assets missing: run `make gui-build` or `pnpm run build` in gui/ first')
    }

    const harness = await startTelemetryHarness(testInfo, mode)
    const { bootstrap, capturePath } = harness
    const events = observeTelemetryRequests(page)
    try {
      await launchBuiltUserApp(page, bootstrap)
      await expect(page.getByRole('option').filter({ hasText: 'skill/skill-000' }).first()).toBeVisible({ timeout: 60_000 })
      const response = page.waitForResponse((candidate) => candidate.request().method() === 'POST' && new URL(candidate.url()).pathname === '/api/telemetry')
      await page.getByRole('option').filter({ hasText: 'skill/skill-001' }).press('Enter')
      expect((await response).status()).toBe(204)
      expect(events.count()).toBe(1)
      await page.screenshot({ path: artifactPath(testInfo, `telemetry-${mode}-browse.png`), fullPage: true, animations: 'disabled' })
    } finally {
      await harness.stop()
    }

    const capture = await readCapture(capturePath)
    expect(capture.spans.filter((span) => span.name === 'agent_marketplace.gui.action')).toHaveLength(0)
    if (mode === 'basic') {
      expect(capture.metrics).toHaveLength(2)
      for (const metric of capture.metrics) {
        expect(metric.attributes).toEqual({
          'command.name': 'gui',
          harnesses: 'devin',
          managed: 'false',
          'outcome.code': 'success',
        })
      }
      expect(capture.requests.metrics).toBeGreaterThan(0)
    }
    if (mode === 'off') {
      expect(capture).toEqual({ spans: [], metrics: [], requests: { traces: 0, metrics: 0 } })
      const stateRoot = join(bootstrap.xdgState!, 'agent-marketplace', 'telemetry')
      expect(existsSync(join(stateRoot, 'installation-id'))).toBe(false)
      expect(existsSync(join(stateRoot, 'queue'))).toBe(false)
    }
  })
}

// spec: gui-shell/gui-actions-use-existing-telemetry-policy
test('built GUI records detailed toggle cancellation spans for explicit Directory action dismissals', async ({ page }, testInfo) => {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run `pnpm run build` in gui/ first')
  }

  const harness = await startWork40TelemetryHarness(testInfo)
  const events = observeTelemetryRequests(page)
  const requests = observeWork40ActionRequests(page)
  const actions: string[] = []
  try {
    const bootstrap = await work40TelemetryBootstrap(harness.bootstrap)
    const paths = work40ProtectedPaths(bootstrap)
    const before = await captureWork40Files(paths)

    await launchBuiltUserApp(page, bootstrap)
    await expect(page.getByRole('option').filter({ hasText: 'skill/review' }).first()).toBeVisible({ timeout: 60_000 })
    const searchButton = page.getByRole('button', { name: 'Search', exact: true })
    await tabTo(page, searchButton, 'Search', actions)
    await page.keyboard.press('Enter')
    const searchInput = page.getByRole('searchbox', { name: 'Search resources' })
    await expect(searchInput).toBeFocused()
    await page.keyboard.type('review')
    await page.keyboard.press('Enter')

    const review = page.getByRole('option').filter({ hasText: 'skill/review' }).first()
    await expect(review).toBeVisible()
    await expect(review).toHaveAttribute('aria-selected', 'true')
    const reviewDOMID = await review.getAttribute('id')
    if (reviewDOMID == null) {
      throw new Error('selected Work40 Review row had no opaque DOM id')
    }
    const reviewByID = page.locator(`[id="${reviewDOMID}"]`)
    await expect(reviewByID).toHaveAttribute('aria-selected', 'true')
    expect(events.count()).toBe(0)
    actions.push('Keyboard-filtered and selected the current Review row without browsing')

    let dialog = await openWork40InstallPlan(page, actions)
    expect(await dialog.locator('input[type="checkbox"]').count()).toBe(0)
    assertWork40PreviewHasNoLocalPaths(await dialog.locator('pre').textContent(), bootstrap)
    await dialog.screenshot({ path: testInfo.outputPath('toggle-cancellation-plan.png'), animations: 'disabled' })

    const escapeResponse = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/telemetry')
    await page.keyboard.press('Escape')
    expect((await escapeResponse).status()).toBe(204)
    await expect(dialog).toBeHidden()
    assertWork40FilesUnchanged(before, await captureWork40Files(paths))
    expect(events.count()).toBe(1)
    actions.push('Dismissed the static Directory plan with Escape; one telemetry response and no protected-file changes')

    dialog = await openWork40InstallPlan(page, actions)
    const declineResponse = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/telemetry')
    await tabTo(page, dialog.getByRole('button', { name: 'Decline', exact: true }), 'Decline', actions, dialog)
    await page.keyboard.press('Enter')
    expect((await declineResponse).status()).toBe(204)
    await expect(dialog).toBeHidden()
    assertWork40FilesUnchanged(before, await captureWork40Files(paths))
    expect(events.count()).toBe(2)
    actions.push('Dismissed the static Directory plan with Decline; one additional telemetry response and no protected-file changes')

    dialog = await openWork40InstallPlan(page, actions)
    const cancelResponse = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/telemetry')
    await tabTo(page, dialog.getByRole('button', { name: 'Cancel', exact: true }), 'Cancel', actions, dialog)
    await page.keyboard.press('Enter')
    expect((await cancelResponse).status()).toBe(204)
    await expect(dialog).toBeHidden()
    assertWork40FilesUnchanged(before, await captureWork40Files(paths))
    expect(events.count()).toBe(3)
    expect({ plans: requests.plans(), resourceActions: requests.resourceActions(), coreActions: requests.coreActions() }).toEqual({ plans: 3, resourceActions: 0, coreActions: 0 })
    actions.push('Dismissed the static Directory plan with Cancel; exactly three plan requests, three telemetry events, and no mutation/core action')
  } finally {
    try {
      await saveWork41Actions(testInfo, actions)
    } finally {
      try {
        await page.close().catch(() => undefined)
      } finally {
        await harness.stop()
      }
    }
  }

  const capture = await readCapture(harness.capturePath)
  const actionSpans = capture.spans.filter((span) => span.name === 'agent_marketplace.gui.action')
  expect(actionSpans).toHaveLength(3)
  for (const span of actionSpans) {
    expect(span.attributes).toEqual({
      action: 'modal_outcome',
      'command.surface': 'gui',
      modal: 'toggle',
      outcome: 'cancelled',
      'resource.kind': 'skill',
    })
  }
  expect(JSON.stringify(actionSpans)).not.toContain('rowId')
  expect(JSON.stringify(actionSpans)).not.toContain('review')
  expect(JSON.stringify(actionSpans)).not.toContain('preview')
  expect(capture.requests.traces).toBeGreaterThan(0)
})
