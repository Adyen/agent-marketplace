// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import WorkspaceView from './WorkspaceView.vue'
import {
  EventCursor,
  RequestError,
  type Client,
  type OperationRef,
  type SessionEvent,
  type Snapshot,
} from './api/client'
import { WorkspaceController, type Row } from './controller'

type RowOverrides = Partial<Row>

function row(name: string, overrides: RowOverrides = {}): Row {
  return {
    id: `skill/${name}#${name.length.toString(16)}`,
    name,
    kind: 'skill',
    description: `${name} description`,
    registry: 'local',
    scope: 'user',
    required: false,
    pendingVerification: false,
    checked: true,
    harnessOverride: false,
    harnesses: ['devin'],
    status: 'enabled',
    ...overrides,
  }
}

function snapshot(rows: Row[], overrides: Partial<Snapshot> = {}): Snapshot {
  return {
    stage: 'final',
    drift: 'ready',
    rows,
    diagnostics: [],
    harnesses: ['devin'],
    operation: null,
    ...overrides,
  }
}

function event(overrides: Partial<SessionEvent>): SessionEvent {
  return { type: 'snapshot', sequence: 0, generation: 1, operation: null, payload: snapshot([]), ...overrides } as SessionEvent
}

function fakeClient() {
  const queue: SessionEvent[] = []
  let pending: ((value: IteratorResult<SessionEvent>) => void) | null = null
  const client: Client = {
    snapshot: vi.fn(async () => snapshot([])),
    events: (cursor: EventCursor = new EventCursor()) => ({
      async *[Symbol.asyncIterator]() {
        for (;;) {
          let next: IteratorResult<SessionEvent>
          if (queue.length > 0) {
            next = { done: false, value: queue.shift()! }
          } else {
            next = await new Promise<IteratorResult<SessionEvent>>((resolve) => {
              pending = resolve
            })
          }
          if (next.done) {
            return
          }
          if (cursor.accept(next.value)) {
            yield next.value
          }
        }
      },
    }),
    action: vi.fn(async () => ({ id: 7, kind: 'sync' }) as OperationRef),
  }
  const push = (e: SessionEvent) => {
    if (pending) {
      const resolve = pending
      pending = null
      resolve({ done: false, value: e })
    } else {
      queue.push(e)
    }
  }
  return { client, push }
}

const cleanups: (() => void)[] = []
afterEach(() => {
  while (cleanups.length > 0) {
    cleanups.pop()!()
  }
  vi.useRealTimers()
})

function mountWorkspace(client: Client, notifyTtl = 60_000) {
  const controller = new WorkspaceController(client, notifyTtl)
  const wrapper = mount(WorkspaceView, { attachTo: document.body, props: { controller } })
  void controller.connect()
  cleanups.push(() => {
    controller.dispose()
    wrapper.unmount()
  })
  return { controller, wrapper }
}

async function tick() {
  await flushPromises()
  await nextTick()
}

describe('WorkspaceView', () => {
  it('groups rows by status with registry-qualified names, scope, and harness badges', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(
      event({
        sequence: 1,
        payload: snapshot([
          row('needed', { required: true, status: 'required (org)', scope: 'organization', registry: 'corp' }),
          row('on', {}),
          row('off', { checked: false, scope: 'available', status: 'available' }),
          row('narrow', { harnessOverride: true, harnesses: ['claude'] }),
          row('broken', { checked: false, status: 'malformed: bad frontmatter' }),
        ]),
      }),
    )
    await tick()

    const headings = wrapper.findAll('h2').map((h) => h.text())
    expect(headings.indexOf('Required')).toBeLessThan(headings.indexOf('Enabled'))
    expect(headings.indexOf('Enabled')).toBeLessThan(headings.indexOf('Available'))

    const text = wrapper.text()
    expect(text).toContain('corp/skill/needed')
    expect(text).toContain('local/skill/broken')
    expect(wrapper.findAll('.workspace__scope').map((s) => s.text())).toContain('organization')
    expect(wrapper.find('.workspace__badge').text()).toContain('claude')
  })

  it('rejects stale generations and retains the accepted list until final', async () => {
    const { client, push } = fakeClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, generation: 1, payload: snapshot([row('first')]) }))
    await tick()
    expect(wrapper.text()).toContain('skill/first')

    push(event({ sequence: 2, generation: 0, payload: snapshot([row('stale')]) }))
    push(event({ sequence: 2, generation: 1, payload: snapshot([row('stale')]) }))
    await tick()
    expect(wrapper.text()).not.toContain('skill/stale')

    push(
      event({
        sequence: 3,
        generation: 2,
        payload: snapshot([row('provisional')], { stage: 'inventory', drift: 'pending' }),
      }),
    )
    await tick()
    expect(wrapper.text()).toContain('skill/first')
    expect(wrapper.text()).not.toContain('skill/provisional')

    push(event({ sequence: 4, generation: 2, payload: snapshot([row('second')]) }))
    await tick()
    expect(wrapper.text()).toContain('skill/second')
    expect(wrapper.text()).not.toContain('skill/first')
    expect(controller.state.operation).toBeNull()
  })

  it('treats the refresh HTTP response as a trigger only; the stream is authoritative', async () => {
    const { client, push } = fakeClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, generation: 1, payload: snapshot([row('first')]) }))
    await tick()

    const staleResponse = snapshot([row('stale-http')], { stage: 'final', drift: 'failed' })
    let resolveSnapshot: (value: Snapshot) => void = () => undefined
    ;(client.snapshot as ReturnType<typeof vi.fn>).mockImplementation(
      () => new Promise<Snapshot>((resolve) => (resolveSnapshot = resolve)),
    )
    const refreshDone = controller.refresh()

    push(event({ sequence: 2, generation: 2, payload: snapshot([row('streamed')]) }))
    await tick()
    resolveSnapshot(staleResponse)
    await refreshDone
    await tick()

    expect(wrapper.text()).toContain('skill/streamed')
    expect(wrapper.text()).not.toContain('skill/stale-http')
    expect(controller.state.drift).toBe('ready')
    expect(controller.state.stage).toBe('final')
  })

  it('keeps the selection stable across an ordinary reload final', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('a'), row('b')]) }))
    await tick()
    const rows = wrapper.findAll('[role="option"]')
    await rows[1].trigger('click')
    expect(rows[1].attributes('aria-selected')).toBe('true')

    push(event({ sequence: 2, generation: 2, payload: snapshot([row('a'), row('b'), row('c')]) }))
    await tick()
    const updated = wrapper.findAll('[role="option"]')
    expect(updated[1].attributes('aria-selected')).toBe('true')
  })

  it('traverses the rendered group order and moves DOM focus on arrows', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(
      event({
        sequence: 1,
        payload: snapshot([row('avail', { checked: false, status: 'available' }), row('req', { required: true })]),
      }),
    )
    await tick()

    const rendered = wrapper.findAll('[role="option"]')
    expect(rendered.map((el) => el.text())).toEqual([
      expect.stringContaining('skill/req'),
      expect.stringContaining('skill/avail'),
    ])

    await rendered[0].trigger('click')
    await rendered[0].trigger('keydown', { key: 'ArrowDown' })
    await tick()
    const options = wrapper.findAll('[role="option"]')
    expect(options[1].attributes('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(options[1].element)

    await options[1].trigger('keydown', { key: 'Enter' })
    await tick()
    expect(wrapper.findAll('[role="option"]')[1].attributes('aria-selected')).toBe('true')
  })

  it('shows generic running when no phase is observed and settles only on final', async () => {
    const { client, push } = fakeClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('a')]) }))
    await tick()

    const operation: OperationRef = { id: 3, kind: 'sync' }
    push(event({ sequence: 2, generation: 2, operation, payload: snapshot([row('a')]) }))
    push(event({ sequence: 3, generation: 2, operation, type: 'progress', payload: { progressSequence: 1, phase: '' } }))
    await tick()
    expect(wrapper.text()).toContain('sync running')

    push(event({ sequence: 4, generation: 2, operation, type: 'progress', payload: { progressSequence: 2, phase: 'materialize' } }))
    await tick()
    expect(wrapper.text()).toContain('sync materialize')

    push(
      event({
        sequence: 5,
        generation: 2,
        operation,
        type: 'notification',
        payload: { severity: 'warning', message: 'still running' },
      }),
    )
    await tick()
    expect(controller.state.operation).toEqual(operation)
    expect(controller.state.phase).toBe('materialize')

    push(event({ sequence: 6, generation: 2, operation: null, payload: snapshot([row('a')]) }))
    push(event({ sequence: 7, generation: 2, operation, type: 'notification', payload: { severity: 'info', message: 'sync complete' } }))
    await tick()
    expect(controller.state.operation).toBeNull()
    expect(controller.state.phase).toBeNull()
  })

  it('does not label a new operation with the previous operation phase', async () => {
    const { client, push } = fakeClient()
    const { controller } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('a')]) }))
    const sync: OperationRef = { id: 5, kind: 'sync' }
    push(event({ sequence: 2, generation: 2, operation: sync, payload: snapshot([row('a')]) }))
    push(event({ sequence: 3, generation: 2, operation: sync, type: 'progress', payload: { progressSequence: 1, phase: 'materialize' } }))
    await tick()
    expect(controller.state.phase).toBe('materialize')

    const update: OperationRef = { id: 6, kind: 'update' }
    push(event({ sequence: 4, generation: 3, operation: update, payload: snapshot([row('a')], { stage: 'final' }) }))
    await tick()
    expect(controller.state.operation).toEqual(update)
    expect(controller.state.phase).toBeNull()
  })

  it('does not resurrect a settled operation from a late action response', async () => {
    const { client, push } = fakeClient()
    const { controller } = mountWorkspace(client)
    const operation: OperationRef = { id: 11, kind: 'sync' }
    ;(client.action as ReturnType<typeof vi.fn>).mockImplementation(async () => {
      push(event({ sequence: 2, generation: 2, operation, payload: snapshot([row('a')]) }))
      push(event({ sequence: 3, generation: 2, operation: null, payload: snapshot([row('a')]) }))
      push(event({ sequence: 4, generation: 2, operation, type: 'notification', payload: { severity: 'info', message: 'done' } }))
      return operation
    })
    push(event({ sequence: 1, payload: snapshot([row('a')]) }))
    await tick()
    await controller.runAction('sync')
    await tick()
    expect(controller.state.operation).toBeNull()
    expect(controller.state.phase).toBeNull()
  })

  it('surfaces a busy rejection truthfully and keeps diagnostics intact', async () => {
    const { client, push } = fakeClient()
    const { controller } = mountWorkspace(client)
    const diagnostic = { severity: 'error' as const, category: 'sync', code: 'x', message: 'existing diagnostic' }
    push(event({ sequence: 1, payload: snapshot([row('a')], { diagnostics: [diagnostic] }) }))
    await tick()

    const action = client.action as ReturnType<typeof vi.fn>
    action.mockRejectedValueOnce(new RequestError(409, 'localapi.busy'))
    await controller.runAction('sync')
    expect(action).toHaveBeenCalledTimes(1)
    expect(controller.state.operation).toBeNull()
    expect(controller.state.diagnostics).toEqual([diagnostic])
    expect(controller.state.error).toBeNull()

    action.mockResolvedValueOnce({ id: 8, kind: 'sync' } as OperationRef)
    await controller.runAction('sync')
    expect(controller.state.operation).toEqual({ id: 8, kind: 'sync' })
  })

  it('reports non-busy action failures without claiming a write', async () => {
    const { client, push } = fakeClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('a')]) }))
    await tick()

    ;(client.action as ReturnType<typeof vi.fn>).mockRejectedValue(new RequestError(400, 'localapi.not_ready'))
    await controller.runAction('sync')
    await tick()
    expect(controller.state.operation).toBeNull()
    expect(controller.state.error).toBe('The action could not be started')
    expect(wrapper.text()).toContain('The action could not be started')
    expect(wrapper.text()).toContain('skill/a')
  })

  it('reports a failed refresh while keeping the accepted rows', async () => {
    const { client, push } = fakeClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('a')]) }))
    await tick()

    ;(client.snapshot as ReturnType<typeof vi.fn>).mockRejectedValue(new RequestError(503, 'localapi.closed'))
    await controller.refresh()
    await tick()
    expect(controller.state.error).toBe('Refresh failed')
    expect(wrapper.text()).toContain('Refresh failed')
    expect(wrapper.text()).toContain('skill/a')
  })

  it('caps notifications at three and expires them on a timer', async () => {
    vi.useFakeTimers()
    const { client, push } = fakeClient()
    const { controller } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('a')]) }))
    for (let i = 0; i < 4; i++) {
      push(
        event({
          sequence: 10 + i,
          generation: 2,
          type: 'notification',
          operation: { id: 50 + i, kind: 'sync' } as OperationRef,
          payload: { severity: 'info', message: `note ${i}` },
        }),
      )
    }
    await vi.advanceTimersByTimeAsync(0)
    await tick()
    expect(controller.state.notifications.map((n) => n.message)).toEqual(['note 1', 'note 2', 'note 3'])
    await vi.advanceTimersByTimeAsync(60_000)
    await tick()
    expect(controller.state.notifications).toEqual([])
  })

  it('disables mutations until a verified inventory with targets and enables them when eligible', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)

    const sync = () => wrapper.find('button[aria-label="Sync"]')
    const update = () => wrapper.find('button[aria-label="Update"]')
    expect((sync().element as HTMLButtonElement).disabled).toBe(true)

    push(event({ sequence: 1, payload: snapshot([row('a')], { stage: 'config', drift: 'pending' }) }))
    await tick()
    expect((sync().element as HTMLButtonElement).disabled).toBe(true)

    push(event({ sequence: 2, payload: snapshot([row('a')], { harnesses: [] }) }))
    await tick()
    expect((sync().element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.text()).toContain('No supported target harnesses are configured')

    push(event({ sequence: 3, generation: 2, payload: snapshot([row('a')], { stage: 'final', drift: 'failed' }) }))
    await tick()
    expect((sync().element as HTMLButtonElement).disabled).toBe(true)

    push(event({ sequence: 4, generation: 3, payload: snapshot([row('a')]) }))
    await tick()
    expect((sync().element as HTMLButtonElement).disabled).toBe(false)
    expect((update().element as HTMLButtonElement).disabled).toBe(false)

    push(event({ sequence: 5, generation: 4, operation: { id: 9, kind: 'sync' } as OperationRef, payload: snapshot([row('a')]) }))
    await tick()
    expect((sync().element as HTMLButtonElement).disabled).toBe(true)
  })

  it('refuses actions on fatal snapshot diagnostics but allows retry after a retained operation error', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    const sync = () => wrapper.find('button[aria-label="Sync"]')
    const update = () => wrapper.find('button[aria-label="Update"]')
    const disabled = () => (sync().element as HTMLButtonElement).disabled

    const fatal = { severity: 'error' as const, category: 'config', code: 'cfg', message: 'broken config' }
    push(event({ sequence: 1, payload: snapshot([row('a')], { stage: 'inventory', diagnostics: [fatal] }) }))
    await tick()
    expect(disabled()).toBe(true)
    expect((update().element as HTMLButtonElement).disabled).toBe(true)

    push(event({ sequence: 2, generation: 2, payload: snapshot([row('a')], { stage: 'inventory' }) }))
    await tick()
    expect(disabled()).toBe(false)

    const opError = { severity: 'error' as const, category: 'sync', code: 'sync-fail', message: 'sync failed' }
    push(event({ sequence: 3, generation: 3, payload: snapshot([row('a')], { diagnostics: [opError] }) }))
    await tick()
    expect(disabled()).toBe(false)
    expect((update().element as HTMLButtonElement).disabled).toBe(false)
  })

  it('exposes named English buttons operable by keyboard', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('a')]) }))
    await tick()

    for (const name of ['Refresh', 'Sync', 'Update']) {
      const button = wrapper.find(`button[aria-label="${name}"]`)
      expect(button.exists()).toBe(true)
      expect(button.element.tagName).toBe('BUTTON')
      expect((button.element as HTMLButtonElement).disabled).toBe(false)
      const element = button.element as HTMLElement
      element.focus()
      expect(document.activeElement).toBe(element)
    }

    const option = wrapper.find('[role="option"]')
    expect(option.exists()).toBe(true)
    await option.trigger('keydown', { key: 'Enter' })
    expect(option.attributes('aria-selected')).toBe('true')
  })
})
