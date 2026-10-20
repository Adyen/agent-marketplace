import { expect, test, type Locator, type Page, type Route, type TestInfo } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'
import { dirname } from 'node:path'
import type { Configuration, OperationRef, ReleaseNotice, SessionEvent, Snapshot, SnapshotRow } from '../src/api/client'
import { serveBuiltApp, distIndexExists } from './app'
import { tabTo } from './keyboard'

const groups: Snapshot['groups'] = [
  { id: 'required', title: 'Required' },
  { id: 'enabled', title: 'Enabled' },
  { id: 'pending', title: 'Pending sync' },
  { id: 'stale', title: 'Stale' },
  { id: 'broken', title: 'Broken' },
  { id: 'available', title: 'Available' },
  { id: 'malformed', title: 'Malformed' },
]

const fixtureConfiguration: Configuration = {
  canInitialize: false,
  consentRequired: false,
  detectedTargets: [],
  detectionUnavailable: false,
  editable: true,
  firstRun: false,
  managedOutsideGUI: [],
  organizationSources: [],
  organizationTargets: [],
  retentionPolicyURL: 'not configured',
  rootPresent: true,
  supportedTargets: ['devin'],
  telemetryDestination: 'not configured',
  userSources: [],
  userTargets: ['devin'],
}

interface SnapshotOptions {
  discoveryEnabled?: boolean
  drift?: Snapshot['drift']
  operation?: OperationRef | null
  settledOperation?: OperationRef | null
}

function resourceRow(name: string, id: string, options: Partial<SnapshotRow> = {}): SnapshotRow {
  const searchName = name.toLowerCase()
  return {
    id,
    name,
    displayName: name,
    searchName,
    searchWords: [searchName],
    kind: 'skill',
    description: `Pending ${name} details`,
    registry: 'source',
    source: { id: 'local', label: '(local)' },
    scope: 'user',
    required: false,
    pendingVerification: true,
    checked: false,
    harnessOverride: false,
    harnesses: ['devin'],
    status: 'pending verification',
    group: 'pending',
    discovery: false,
    inspectable: false,
    installedFiles: false,
    resourceAction: 'none',
    ...options,
  }
}

function initialRows(): SnapshotRow[] {
  return Array.from({ length: 40 }, (_, index) => {
    const name = `resource-${String(index).padStart(2, '0')}`
    return resourceRow(name, `opaque-user-source-${name}`)
  })
}

function snapshotPayload(stage: Snapshot['stage'], rows: SnapshotRow[], options: SnapshotOptions = {}): Snapshot {
  const operation = options.operation ?? null
  return {
    stage,
    drift: options.drift ?? (stage === 'final' ? 'ready' : 'pending'),
    rows,
    groups,
    diagnostics: [],
    harnesses: ['devin'],
    pendingChanges: 0,
    discoveryEnabled: options.discoveryEnabled ?? false,
    discoveryHealth: [],
    discoveryQuery: '',
    discoveryRequestId: 0,
    discoverySequence: 0,
    discoveryLoading: false,
    operation,
    settledOperation: options.settledOperation ?? null,
  }
}

function snapshotEvent(
  sequence: number,
  generation: number,
  stage: Snapshot['stage'],
  rows: SnapshotRow[],
  options: SnapshotOptions = {},
): SessionEvent {
  return {
    type: 'snapshot',
    sequence,
    generation,
    operation: options.operation ?? null,
    payload: snapshotPayload(stage, rows, options),
  }
}

function discoveryEvent(sequence: number, generation: number, rows: SnapshotRow[]): SessionEvent {
  return {
    type: 'discovery',
    sequence,
    generation,
    operation: null,
    payload: { requestId: 0, query: '', discoverySequence: 1, rows, health: [], loading: false },
  }
}

function relativeTop(locator: Locator): Promise<number> {
  return locator.evaluate((element) => {
    const root = element.closest('.workspace__list')
    if (!(root instanceof HTMLElement)) {
      throw new Error('selected resource has no list scroll root')
    }
    return element.getBoundingClientRect().top - root.getBoundingClientRect().top
  })
}

async function saveActions(testInfo: TestInfo, filename: string, actions: string[]): Promise<void> {
  const path = testInfo.outputPath(filename)
  await mkdir(dirname(path), { recursive: true })
  await writeFile(path, `${JSON.stringify({ actions }, null, 2)}\n`)
}

interface HeldEventResponse {
  send(events: SessionEvent[]): Promise<void>
  cancel(): Promise<void>
}

function createEventBarrier() {
  const queued: HeldEventResponse[] = []
  const waiters: ((response: HeldEventResponse | null) => void)[] = []
  const active = new Set<HeldEventResponse>()
  let closed = false

  const hold = async (route: Route): Promise<void> => {
    if (closed) {
      await route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store' }, body: '' })
      return
    }
    let resolveFinished: () => void = () => undefined
    const finished = new Promise<void>((resolve) => (resolveFinished = resolve))
    let done = false
    let held: HeldEventResponse
    const finish = () => {
      if (done) {
        return
      }
      done = true
      active.delete(held)
      resolveFinished()
    }
    held = {
      async send(events) {
        if (done) {
          return
        }
        const body = events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join('')
        try {
          await route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store' }, body })
        } finally {
          finish()
        }
      },
      async cancel() {
        if (done) {
          return
        }
        try {
          await route.abort()
        } catch {
        } finally {
          finish()
        }
      },
    }
    active.add(held)
    const waiter = waiters.shift()
    if (waiter == null) {
      queued.push(held)
    } else {
      waiter(held)
    }
    await finished
  }

  const next = async (): Promise<HeldEventResponse> => {
    const response = queued.shift()
    if (response != null) {
      return response
    }
    if (closed) {
      throw new Error('event route was closed before the next stage arrived')
    }
    return await new Promise<HeldEventResponse>((resolve, reject) => {
      waiters.push((value) => {
        if (value == null) {
          reject(new Error('event route was closed while waiting for a stage'))
        } else {
          resolve(value)
        }
      })
    })
  }

  const close = async (): Promise<void> => {
    if (closed) {
      return
    }
    closed = true
    for (const waiter of waiters.splice(0)) {
      waiter(null)
    }
    await Promise.all([...active].map((response) => response.cancel()))
  }

  return { hold, next, close }
}

async function fulfillJSON(route: Route, body: unknown): Promise<void> {
  await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
}

async function startCannedWorkspace(page: Page) {
  if (!distIndexExists()) {
    throw new Error('built GUI assets missing: run the single GUI build before the startup journey')
  }
  const url = 'http://127.0.0.1:43871'
  const credential = 'work38-fixture-credential'
  const events = createEventBarrier()
  const calls = { operations: 0, discovery: 0, telemetry: 0, unexpected: [] as string[] }
  await serveBuiltApp(page, url)
  await page.route(`${url}/api/**`, async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (request.method() !== 'POST' || request.headers()['x-agent-marketplace-token'] !== credential) {
      calls.unexpected.push('API boundary mismatch')
      await route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ code: 'unauthorized' }) })
      return
    }
    switch (pathname) {
      case '/api/events':
        await events.hold(route)
        return
      case '/api/configuration':
        await fulfillJSON(route, fixtureConfiguration)
        return
      case '/api/release':
        await fulfillJSON(route, { latestVersion: '' } satisfies ReleaseNotice)
        return
      case '/api/snapshot':
        await fulfillJSON(route, snapshotPayload('config', []))
        return
      case '/api/telemetry':
        calls.telemetry++
        await route.fulfill({ status: 204 })
        return
      case '/api/discovery':
        calls.discovery++
        await route.fulfill({ status: 204 })
        return
      case '/api/actions':
      case '/api/configuration/actions':
        calls.operations++
        await route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ code: 'localapi.busy' }) })
        return
      default:
        calls.unexpected.push(pathname)
        await route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ code: 'not_found' }) })
    }
  })
  await page.addInitScript(
    ([bootstrapURL, token]) => {
      ;(window as unknown as { __AM_GUI_BOOTSTRAP__?: { url: string; credential: string; version: string } }).__AM_GUI_BOOTSTRAP__ = {
        url: bootstrapURL,
        credential: token,
        version: 'work38-fixture',
      }
    },
    [url, credential] as const,
  )
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto(url, { waitUntil: 'domcontentloaded' })
  return { events, calls }
}

// spec: session-startup/selection-follows-its-resource-when-rows-are-inserted
// spec: session-startup/an-open-detail-follows-its-verified-row
test('built GUI preserves a keyboard-selected pending Resource and its viewport across startup stages', async ({ page }, testInfo) => {
  const actions: string[] = []
  const fixture = await startCannedWorkspace(page)
  const operation: OperationRef = { id: 91, kind: 'sync' }
  const rows = initialRows()
  const targetID = rows[19].id

  try {
    const resources = page.getByRole('region', { name: 'Resources' })
    const detail = page.getByRole('complementary', { name: 'Detail' })
    await expect(resources).toBeVisible()
    const config = await fixture.events.next()
    await config.send([snapshotEvent(1, 7, 'config', rows, { operation })])
    await expect(page.getByRole('option')).toHaveCount(40)
    await expect(detail).toContainText('Pending resource-00 details')
    await expect(detail).toContainText('Drift: pending')

    const first = page.getByRole('option').nth(0)
    await tabTo(page, first, 'first resource row', actions)
    for (let index = 0; index < 19; index++) {
      await page.keyboard.press('ArrowDown')
    }
    const selected = page.getByRole('option').filter({ hasText: 'resource-19' })
    await expect(selected).toHaveAttribute('aria-selected', 'true')
    await expect(detail).toContainText('Pending resource-19 details')
    const selectedDOMID = await selected.getAttribute('id')
    if (selectedDOMID == null) {
      throw new Error('selected pending resource has no stable DOM identity')
    }
    const topBefore = await relativeTop(selected)
    actions.push('Used Tab and ArrowDown to select the twentieth pending resource')
    await page.screenshot({ path: testInfo.outputPath('startup-selected-config.png'), fullPage: true, animations: 'disabled' })

    const inventoryRows = [
      ...Array.from({ length: 10 }, (_, index) => resourceRow(`aaa-inventory-${String(index).padStart(2, '0')}`, `opaque-inventory-${index}`)),
      ...rows,
    ]
    const inventory = await fixture.events.next()
    await inventory.send([snapshotEvent(2, 7, 'inventory', inventoryRows, { operation })])
    await expect(page.getByRole('option')).toHaveCount(50)
    await expect(selected).toHaveAttribute('id', selectedDOMID)
    await expect(selected).toHaveAttribute('aria-selected', 'true')
    await expect(detail).toContainText('Pending resource-19 details')
    const topAfterInventory = await relativeTop(selected)
    expect(Math.abs(topAfterInventory - topBefore)).toBeLessThanOrEqual(1)
    actions.push('Inventory inserted rows above the selected Resource; row identity, detail, and viewport line stayed stable')
    await page.screenshot({ path: testInfo.outputPath('startup-selected-inventory.png'), fullPage: true, animations: 'disabled' })

    const verifiedRows = inventoryRows.map((candidate) => candidate.id === targetID
      ? { ...candidate, description: 'Verified resource-19 details', pendingVerification: false, status: 'pending sync' }
      : candidate)
    const finalRows = [
      ...Array.from({ length: 4 }, (_, index) => resourceRow(`aab-final-${String(index).padStart(2, '0')}`, `opaque-final-${index}`)),
      ...verifiedRows,
    ]
    const final = await fixture.events.next()
    await final.send([snapshotEvent(3, 7, 'final', finalRows, { settledOperation: operation })])
    await expect(page.getByRole('option')).toHaveCount(54)
    await expect(selected).toHaveAttribute('id', selectedDOMID)
    await expect(selected).toHaveAttribute('aria-selected', 'true')
    await expect(detail).toContainText('Verified resource-19 details')
    const topAfterFinal = await relativeTop(selected)
    expect(Math.abs(topAfterFinal - topBefore)).toBeLessThanOrEqual(1)
    actions.push('Final verification updated the same row identity and aside metadata without shifting the selected screen line')
    await page.screenshot({ path: testInfo.outputPath('startup-selected-final.png'), fullPage: true, animations: 'disabled' })
    expect(fixture.calls.operations).toBe(0)
    expect(fixture.calls.discovery).toBe(0)
    expect(fixture.calls.unexpected).toEqual([])
  } finally {
    await fixture.events.close()
    await saveActions(testInfo, 'startup-selected.actions.json', actions)
  }
})

// spec: session-startup/untouched-selection-remains-at-the-top-as-rows-arrive
test('built GUI keeps untouched startup selection at the first row and viewport top', async ({ page }, testInfo) => {
  const actions: string[] = []
  const fixture = await startCannedWorkspace(page)
  const localRows = initialRows()
  const discoveryRow = resourceRow('cached-resource', 'opaque-discovery-resource', {
    registry: 'marketplace',
    scope: 'available',
    group: 'available',
    status: 'available',
    pendingVerification: false,
    checked: false,
    discovery: true,
  })
  const discoveryRows = [...localRows, discoveryRow]

  try {
    const resources = page.getByRole('region', { name: 'Resources' })
    await expect(resources).toBeVisible()
    const config = await fixture.events.next()
    await config.send([snapshotEvent(1, 7, 'config', localRows, { discoveryEnabled: true })])
    await expect(page.getByRole('option')).toHaveCount(40)
    const firstConfig = page.getByRole('option').nth(0)
    await expect(firstConfig).toContainText('resource-00')
    await expect(firstConfig).toHaveAttribute('aria-selected', 'true')
    await expect(resources).toHaveJSProperty('scrollTop', 0)
    await page.screenshot({ path: testInfo.outputPath('startup-untouched-config.png'), fullPage: true, animations: 'disabled' })

    const discovery = await fixture.events.next()
    await discovery.send([discoveryEvent(2, 7, discoveryRows)])
    await expect(page.getByRole('option')).toHaveCount(41)
    await expect(page.getByRole('option').filter({ hasText: 'cached-resource' })).toBeVisible()
    const firstDiscovery = page.getByRole('option').nth(0)
    await expect(firstDiscovery).toContainText('resource-00')
    await expect(firstDiscovery).toHaveAttribute('aria-selected', 'true')
    await expect(resources).toHaveJSProperty('scrollTop', 0)
    actions.push('Discovery rows arrived over SSE without any user selection or scroll interaction')
    await page.screenshot({ path: testInfo.outputPath('startup-untouched-discovery.png'), fullPage: true, animations: 'disabled' })

    const requiredInventory = resourceRow('aaa-required-inventory', 'opaque-required-inventory', {
      group: 'required',
      status: 'required',
      required: true,
      pendingVerification: false,
      checked: true,
    })
    const inventoryRows = [requiredInventory, ...discoveryRows]
    const inventory = await fixture.events.next()
    await inventory.send([snapshotEvent(3, 7, 'inventory', inventoryRows, { discoveryEnabled: true })])
    await expect(page.getByRole('option')).toHaveCount(42)
    const firstInventory = page.getByRole('option').nth(0)
    await expect(firstInventory).toContainText('aaa-required-inventory')
    await expect(firstInventory).toHaveAttribute('aria-selected', 'true')
    await expect(resources).toHaveJSProperty('scrollTop', 0)
    await page.screenshot({ path: testInfo.outputPath('startup-untouched-inventory.png'), fullPage: true, animations: 'disabled' })

    const requiredFinal = resourceRow('000-required-final', 'opaque-required-final', {
      group: 'required',
      status: 'required',
      required: true,
      pendingVerification: false,
      checked: true,
    })
    const final = await fixture.events.next()
    await final.send([snapshotEvent(4, 7, 'final', [requiredFinal, ...inventoryRows], { discoveryEnabled: true })])
    await expect(page.getByRole('option')).toHaveCount(43)
    const firstFinal = page.getByRole('option').nth(0)
    await expect(firstFinal).toContainText('000-required-final')
    await expect(firstFinal).toHaveAttribute('aria-selected', 'true')
    await expect(resources).toHaveJSProperty('scrollTop', 0)
    actions.push('Inventory and final replacements kept the untouched first visible Resource at the top')
    await page.screenshot({ path: testInfo.outputPath('startup-untouched-final.png'), fullPage: true, animations: 'disabled' })
    expect(fixture.calls.operations).toBe(0)
    expect(fixture.calls.unexpected).toEqual([])
  } finally {
    await fixture.events.close()
    await saveActions(testInfo, 'startup-untouched.actions.json', actions)
  }
})
