import { test, expect, type Page, type TestInfo } from '@playwright/test'
import { existsSync } from 'node:fs'
import { mkdir, readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { startHarness, type Bootstrap } from './harness'
import { distIndexExists, serveBuiltApp } from './app'

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
    AM_LOCALAPI_USER_FIXTURE: 'telemetry',
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
    await expect(page.getByText('skill/solo')).toBeVisible({ timeout: 60_000 })

    const quit = page.getByRole('button', { name: 'Quit', exact: true })
    await quit.press('Enter')
    const dialog = page.getByRole('dialog', { name: 'Quit Agent Marketplace' })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Sync and quit' })).toBeVisible()
    const cancelResponse = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/telemetry')
    await dialog.getByRole('button', { name: 'Cancel quit' }).press('Enter')
    expect((await cancelResponse).status()).toBe(204)
    await expect(dialog).not.toBeVisible()

    const resource = page.getByRole('option').filter({ hasText: 'skill/solo' })
    const browseResponse = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/telemetry')
    await resource.press('Enter')
    expect((await browseResponse).status()).toBe(204)
    await expect(page.getByRole('complementary', { name: 'Detail' }).getByRole('heading').first()).toContainText('skill/solo')

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
      await expect(page.getByText('skill/solo')).toBeVisible({ timeout: 60_000 })
      const response = page.waitForResponse((candidate) => candidate.request().method() === 'POST' && new URL(candidate.url()).pathname === '/api/telemetry')
      await page.getByRole('option').filter({ hasText: 'skill/solo' }).press('Enter')
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
