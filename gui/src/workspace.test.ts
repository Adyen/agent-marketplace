// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import {
  EventCursor,
  RequestError,
  type Client,
  type OperationRef,
  type SessionEvent,
  type Snapshot,
} from './api/client'
import {
  EVENT_OUTAGE_MESSAGE,
  EVENT_RETRY_DELAY_MS,
  WorkspaceController,
} from './controller'
import { event, fakeClient, mountWorkspace, row, snapshot, tick } from './test-support'

describe('WorkspaceView', () => {
  it('groups rows by status with registry-qualified names, scope, and harness badges', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(
      event({
        sequence: 1,
        payload: snapshot([
          row('needed', { group: 'required', required: true, status: 'required (org)', scope: 'organization', registry: 'corp' }),
          row('on', { group: 'enabled' }),
          row('off', { group: 'available', checked: false, scope: 'available', status: 'available' }),
          row('narrow', { group: 'enabled', harnessOverride: true, harnesses: ['claude'] }),
          row('broken', { group: 'malformed', checked: false, status: 'malformed: bad frontmatter' }),
        ]),
      }),
    )
    await tick()

    const groups = wrapper.findAll('.workspace__group-toggle').map((button) => button.attributes('aria-label'))
    expect(groups).toEqual(['Required', 'Enabled', 'Available', 'Malformed'])

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

  // spec: session-navigation/status-grouped-resource-order-and-filtering
  it('traverses visible section headers and rows on arrows', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(
      event({
        sequence: 1,
        payload: snapshot([row('avail', { group: 'available', checked: false, status: 'available' }), row('req', { group: 'required', required: true, status: 'required' })]),
      }),
    )
    await tick()

    const options = wrapper.findAll('[role="option"]')
    expect(options.map((el) => el.text())).toEqual([
      expect.stringContaining('skill/req'),
      expect.stringContaining('skill/avail'),
    ])
    expect(options[0].attributes('aria-selected')).toBe('true')

    await options[0].trigger('keydown', { key: 'ArrowDown' })
    await tick()
    const availableSection = wrapper.find('button[aria-label="Available"]')
    expect(availableSection.attributes('aria-current')).toBe('true')
    expect(document.activeElement).toBe(availableSection.element)

    await availableSection.trigger('keydown', { key: 'ArrowDown' })
    await tick()
    const updatedOptions = wrapper.findAll('[role="option"]')
    expect(updatedOptions[1].attributes('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(updatedOptions[1].element)

    await updatedOptions[1].trigger('keydown', { key: 'Enter' })
    await tick()
    expect(wrapper.findAll('[role="option"]')[1].attributes('aria-selected')).toBe('true')
  })

  // spec: gui-shell/gui-actions-use-existing-telemetry-policy
  it('reports browse once for each new selection across click, Enter, and Space', async () => {
    const { client, push } = fakeClient()
    const first = row('first')
    const second = row('second')
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([first, second]) }))
    await tick()

    const options = wrapper.findAll('[role="option"]')
    expect(options[0].attributes('aria-selected')).toBe('true')
    await options[1].trigger('click')
    await options[0].trigger('keydown', { key: 'Enter' })
    await options[1].trigger('keydown', { key: ' ' })
    await options[1].trigger('click')
    await tick()

    expect(client.telemetry).toHaveBeenCalledTimes(3)
    expect(client.telemetry).toHaveBeenNthCalledWith(1, { action: 'browse', rowId: second.id })
    expect(client.telemetry).toHaveBeenNthCalledWith(2, { action: 'browse', rowId: first.id })
    expect(client.telemetry).toHaveBeenNthCalledWith(3, { action: 'browse', rowId: second.id })
    expect(options[1].attributes('aria-selected')).toBe('true')
  })

  // spec: gui-shell/gui-actions-use-existing-telemetry-policy
  it('reports arrow selection only when the Resource detail actually changes', async () => {
    const { client, push } = fakeClient()
    const first = row('first')
    const second = row('second')
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([first, second]) }))
    await tick()

    const options = wrapper.findAll('[role="option"]')
    expect(options[0].attributes('aria-selected')).toBe('true')
    await options[0].trigger('click')
    await options[0].trigger('keydown', { key: 'ArrowDown' })
    await tick()
    await wrapper.findAll('[role="option"]')[1].trigger('keydown', { key: 'ArrowDown' })
    await tick()

    expect(client.telemetry).toHaveBeenCalledTimes(1)
    expect(client.telemetry).toHaveBeenCalledWith({ action: 'browse', rowId: second.id })
    expect(wrapper.findAll('[role="option"]')[1].attributes('aria-selected')).toBe('true')
  })

  // spec: gui-shell/gui-actions-use-existing-telemetry-policy
  it('keeps the view and selected Resource unchanged when browse telemetry fails', async () => {
    const { client, push } = fakeClient()
    client.telemetry = vi.fn(async () => {
      throw new Error('telemetry unavailable')
    })
    const first = row('first')
    const selected = row('solo')
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([first, selected]) }))
    await tick()

    await wrapper.findAll('[role="option"]')[1].trigger('keydown', { key: 'Enter' })
    await tick()

    expect(client.telemetry).toHaveBeenCalledWith({ action: 'browse', rowId: selected.id })
    expect(wrapper.findAll('[role="option"]')[1].attributes('aria-selected')).toBe('true')
    expect(wrapper.text()).toContain('skill/solo')
    expect(controller.state.error).toBeNull()
    expect(controller.state.requesting).toBe(false)
    expect(controller.state.operation).toBeNull()
  })

  // spec: gui-shell/gui-actions-use-existing-telemetry-policy
  it('leaves sync and update telemetry to the accepted local API action', async () => {
    const { client, push } = fakeClient()
    client.action = vi.fn(async (action: Parameters<Client['action']>[0]) => {
      return action === 'update' ? ({ id: 8, kind: 'update' } as OperationRef) : ({ id: 7, kind: 'sync' } as OperationRef)
    })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('solo')]) }))
    await tick()

    await wrapper.find('button[aria-label="Sync"]').trigger('click')
    await tick()
    push(event({ sequence: 2, generation: 2, payload: snapshot([row('solo')], { settledOperation: { id: 7, kind: 'sync' } }) }))
    await tick()
    await wrapper.find('button[aria-label="Update"]').trigger('click')
    await tick()

    expect(client.action).toHaveBeenNthCalledWith(1, 'sync')
    expect(client.action).toHaveBeenNthCalledWith(2, 'update')
    expect(client.telemetry).not.toHaveBeenCalled()
  })

  // spec: session-feedback/phase-observed-before-completion
  it('keeps accepting observed phases after a notification without settling the operation', async () => {
    const { client, push } = fakeClient()
    const { controller, wrapper } = mountWorkspace(client)
    const operation: OperationRef = { id: 3, kind: 'update' }
    const diagnostic = { severity: 'warning' as const, category: 'catalog', code: 'kept', message: 'Existing warning' }
    push(event({ sequence: 1, generation: 2, operation, payload: snapshot([row('a')], { diagnostics: [diagnostic] }) }))
    push(event({ sequence: 2, generation: 2, operation, type: 'progress', payload: { progressSequence: 1, phase: 'resolve' } }))
    await tick()
    expect(wrapper.text()).toContain('update resolve')
    push(event({ sequence: 3, generation: 2, operation, type: 'notification', payload: { severity: 'warning', message: 'Still working' } }))
    push(event({ sequence: 4, generation: 2, operation, type: 'progress', payload: { progressSequence: 2, phase: 'materialize' } }))
    await tick()
    expect(wrapper.text()).toContain('update materialize')
    expect(controller.state.operation).toEqual(operation)
    expect(controller.state.diagnostics).toEqual([diagnostic])
    expect(wrapper.find('button[aria-label="Update"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).not.toContain('update complete')
  })

  // spec: session-feedback/late-progress
  it('late-progress leaves mounted feedback unchanged for stale phases', async () => {
    const { client, push } = fakeClient()
    const { controller, wrapper } = mountWorkspace(client)
    const operation: OperationRef = { id: 30, kind: 'update' }
    const diagnostic = { severity: 'warning' as const, category: 'catalog', code: 'retained', message: 'Retained warning' }
    const notifications = () => wrapper.findAll('ul[aria-label="Notifications"] li')
    const expectCurrentFeedback = () => {
      expect(controller.state.operation).toEqual(operation)
      expect(controller.state.phase).toBe('materialize')
      expect(controller.state.diagnostics).toEqual([diagnostic])
      expect(wrapper.find('ul[aria-label="Diagnostics"]').text()).toContain('Retained warning')
      expect(wrapper.findAll('[role="status"]').map((status) => status.text())).toContain('update materialize')
      expect(notifications()).toHaveLength(0)
    }

    push(event({ sequence: 1, generation: 2, operation, payload: snapshot([row('a')], { operation, diagnostics: [diagnostic] }) }))
    push(event({ sequence: 2, generation: 2, operation, type: 'progress', payload: { progressSequence: 1, phase: 'resolve' } }))
    await tick()
    expect(wrapper.findAll('[role="status"]').map((status) => status.text())).toContain('update resolve')
    expect(controller.state.operation).toEqual(operation)
    expect(controller.state.diagnostics).toEqual([diagnostic])
    expect(wrapper.find('ul[aria-label="Diagnostics"]').text()).toContain('Retained warning')

    push(event({ sequence: 3, generation: 2, operation, type: 'progress', payload: { progressSequence: 2, phase: 'materialize' } }))
    await tick()
    expectCurrentFeedback()

    push(event({ sequence: 4, generation: 2, operation, type: 'progress', payload: { progressSequence: 2, phase: 'duplicate-phase-sentinel' } }))
    await tick()
    expectCurrentFeedback()

    push(event({ sequence: 5, generation: 2, operation, type: 'progress', payload: { progressSequence: 1, phase: 'lower-sequence-sentinel' } }))
    await tick()
    expectCurrentFeedback()

    push(event({ sequence: 6, generation: 2, operation: { id: 31, kind: 'update' }, type: 'progress', payload: { progressSequence: 3, phase: 'wrong-operation-sentinel' } }))
    await tick()
    expectCurrentFeedback()

    push(event({ sequence: 7, generation: 1, operation, type: 'progress', payload: { progressSequence: 3, phase: 'older-generation-sentinel' } }))
    await tick()
    expectCurrentFeedback()

    push(event({ sequence: 8, generation: 2, operation: null, payload: snapshot([row('a')], { diagnostics: [diagnostic] }) }))
    await tick()
    expect(controller.state.operation).toBeNull()
    expect(controller.state.phase).toBeNull()
    expect(controller.state.diagnostics).toEqual([diagnostic])
    expect(notifications()).toHaveLength(0)

    push(event({ sequence: 9, generation: 2, operation, type: 'progress', payload: { progressSequence: 3, phase: 'post-final-sentinel' } }))
    await tick()
    expect(controller.state.operation).toBeNull()
    expect(controller.state.phase).toBeNull()
    expect(controller.state.diagnostics).toEqual([diagnostic])
    expect(notifications()).toHaveLength(0)
    expect(wrapper.text()).not.toContain('post-final-sentinel')
  })

  // spec: session-feedback/no-observer
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

  it('reports acceptance of a settled operation without resurrecting its state', async () => {
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
    const accepted = await controller.runAction('sync')
    await tick()
    expect(accepted).toBe(true)
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

  // spec: session-feedback/notification-overflow
  it('notification-overflow caps mounted notices and expires them with controls usable', async () => {
    vi.useFakeTimers()
    const { client, push } = fakeClient()
    const { controller, wrapper } = mountWorkspace(client)
    const notifications = () => wrapper.findAll('ul[aria-label="Notifications"] li')
    const expectRequiredControlsUsable = () => {
      for (const label of ['Sync', 'Update']) {
        const control = wrapper.find(`button[aria-label="${label}"]`)
        expect(control.exists()).toBe(true)
        expect(control.isVisible()).toBe(true)
        expect((control.element as HTMLButtonElement).disabled).toBe(false)
      }
    }

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
    expect(notifications()).toHaveLength(3)
    expect(notifications().map((notice) => notice.text())).toEqual(['note 1', 'note 2', 'note 3'])
    expect(notifications().every((notice) => notice.isVisible())).toBe(true)
    expectRequiredControlsUsable()

    await vi.advanceTimersByTimeAsync(60_000)
    await tick()
    expect(controller.state.notifications).toEqual([])
    expect(notifications()).toHaveLength(0)
    expectRequiredControlsUsable()
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

interface StreamChannel {
  push(e: SessionEvent): void
  end(): void
  fail(err: unknown): void
}

function streamClient(): { client: Client; channels: StreamChannel[] } {
  const { client } = fakeClient()
  const channels: StreamChannel[] = []
  client.events = (cursor: EventCursor = new EventCursor(), signal?: AbortSignal) => {
    const results: IteratorResult<SessionEvent>[] = []
    let pending: ((r: IteratorResult<SessionEvent>) => void) | null = null
    let failure: unknown = null
    const deliver = (r: IteratorResult<SessionEvent>) => {
      if (pending != null) {
        const resolve = pending
        pending = null
        resolve(r)
      } else {
        results.push(r)
      }
    }
    channels.push({
      push: (e) => deliver({ done: false, value: e }),
      end: () => deliver({ done: true, value: undefined }),
      fail: (err) => {
        failure = err
        deliver({ done: true, value: undefined })
      },
    })
    signal?.addEventListener('abort', () => deliver({ done: true, value: undefined }), { once: true })
    return {
      async *[Symbol.asyncIterator]() {
        for (;;) {
          const next =
            results.length > 0
              ? results.shift()!
              : await new Promise<IteratorResult<SessionEvent>>((resolve) => {
                  pending = resolve
                })
          if (failure != null) {
            throw failure
          }
          if (next.done) {
            return
          }
          if (cursor.accept(next.value)) {
            yield next.value
          }
        }
      },
    }
  }
  return { client, channels }
}

describe('Event stream reconnect', () => {
  it('resubscribes after a clean end and clears the outage only on an accepted final', async () => {
    vi.useFakeTimers()
    const { client, channels } = streamClient()
    const controller = new WorkspaceController(client)
    const connect = controller.connect()

    channels[0].push(event({ sequence: 1, payload: snapshot([row('on')]) }))
    await tick()
    expect(controller.state.rows).toHaveLength(1)
    expect(controller.state.drift).toBe('ready')

    channels[0].end()
    await tick()
    expect(controller.state.error).toBe(EVENT_OUTAGE_MESSAGE)
    expect(controller.state.rows).toHaveLength(1)
    expect(controller.state.drift).toBe('ready')
    expect(channels).toHaveLength(1)

    await vi.advanceTimersByTimeAsync(EVENT_RETRY_DELAY_MS)
    await tick()
    expect(channels).toHaveLength(2)
    expect(controller.state.error).toBe(EVENT_OUTAGE_MESSAGE)

    channels[1].push(event({ sequence: 1, payload: snapshot([], { drift: 'failed' }) }))
    await tick()
    expect(controller.state.error).toBe(EVENT_OUTAGE_MESSAGE)
    expect(controller.state.rows).toHaveLength(1)
    expect(controller.state.drift).toBe('ready')

    channels[1].push(event({ sequence: 2, payload: snapshot([row('on'), row('extra')]) }))
    await tick()
    expect(controller.state.error).toBeNull()
    expect(controller.state.rows).toHaveLength(2)
    controller.dispose()
    await connect
  })

  it('resubscribes after a stream error and holds the outage across failed attempts', async () => {
    vi.useFakeTimers()
    const { client, channels } = streamClient()
    const controller = new WorkspaceController(client)
    const connect = controller.connect()

    channels[0].push(event({ sequence: 1 }))
    await tick()
    channels[0].fail(new Error('stream reset'))
    await tick()
    expect(controller.state.error).toBe(EVENT_OUTAGE_MESSAGE)

    await vi.advanceTimersByTimeAsync(EVENT_RETRY_DELAY_MS)
    await tick()
    expect(channels).toHaveLength(2)
    channels[1].fail(new Error('still down'))
    await tick()
    expect(controller.state.error).toBe(EVENT_OUTAGE_MESSAGE)

    await vi.advanceTimersByTimeAsync(EVENT_RETRY_DELAY_MS)
    await tick()
    expect(channels).toHaveLength(3)
    channels[2].push(event({ sequence: 2 }))
    await tick()
    expect(controller.state.error).toBeNull()
    controller.dispose()
    await connect
  })

  it('never clears an unrelated action error on recovery', async () => {
    vi.useFakeTimers()
    const { client, channels } = streamClient()
    const controller = new WorkspaceController(client)
    const connect = controller.connect()

    channels[0].push(event({ sequence: 1 }))
    await tick()
    channels[0].end()
    await tick()
    expect(controller.state.error).toBe(EVENT_OUTAGE_MESSAGE)
    controller.state.error = 'The action could not be started'

    await vi.advanceTimersByTimeAsync(EVENT_RETRY_DELAY_MS)
    await tick()
    channels[1].push(event({ sequence: 2 }))
    await tick()
    expect(controller.state.error).toBe('The action could not be started')
    controller.dispose()
    await connect
  })

  it('stops resubscribing and cancels the pending retry after dispose', async () => {
    vi.useFakeTimers()
    const { client, channels } = streamClient()
    const controller = new WorkspaceController(client)
    const connect = controller.connect()

    channels[0].push(event({ sequence: 1 }))
    await tick()
    channels[0].end()
    await tick()
    expect(controller.state.error).toBe(EVENT_OUTAGE_MESSAGE)

    controller.dispose()
    await vi.advanceTimersByTimeAsync(EVENT_RETRY_DELAY_MS * 4)
    await tick()
    expect(channels).toHaveLength(1)
    await connect
  })

  it.each([400, 401, 403, 405])('does not retry a fatal %d stream rejection', async (status) => {
    vi.useFakeTimers()
    const { client, channels } = streamClient()
    const controller = new WorkspaceController(client)
    const connect = controller.connect()

    const failure = new RequestError(status, 'localapi.unauthorized')
    channels[0].fail(failure)
    await expect(connect).rejects.toBe(failure)
    await vi.advanceTimersByTimeAsync(EVENT_RETRY_DELAY_MS * 4)
    expect(channels).toHaveLength(1)
    expect(controller.state.error).not.toBe(EVENT_OUTAGE_MESSAGE)
    controller.dispose()
  })

  it('settles a missed operation from the replayed marker and ignores its late acceptance', async () => {
    vi.useFakeTimers()
    const { client, channels } = streamClient()
    const controller = new WorkspaceController(client)
    const connect = controller.connect()

    channels[0].push(event({ sequence: 1 }))
    await tick()
    channels[0].end()
    await tick()
    await vi.advanceTimersByTimeAsync(EVENT_RETRY_DELAY_MS)
    await tick()

    channels[1].push(
      event({
        sequence: 2,
        generation: 2,
        operation: null,
        payload: snapshot([], { settledOperation: { id: 9, kind: 'mutation' } }),
      }),
    )
    await tick()
    expect(controller.state.operation).toBeNull()
    expect(client.configuration).toHaveBeenCalled()

    const configurationCalls = (client.configuration as ReturnType<typeof vi.fn>).mock.calls.length
    channels[1].push(
      event({
        sequence: 3,
        generation: 3,
        operation: null,
        payload: snapshot([], { settledOperation: { id: 9, kind: 'mutation' } }),
      }),
    )
    await tick()
    expect((client.configuration as ReturnType<typeof vi.fn>).mock.calls.length).toBe(
      configurationCalls,
    )

    ;(client.configure as ReturnType<typeof vi.fn>).mockResolvedValue({
      id: 9,
      kind: 'mutation',
    } as OperationRef)
    const accepted = await controller.removeSource('user-dir-id')
    expect(accepted).toBe(true)
    await tick()
    expect(controller.state.operation).toBeNull()
    expect(controller.state.phase).toBeNull()
    controller.dispose()
    await connect
  })

  it('preserves an active operation newer than a replayed settlement', async () => {
    const { client, channels } = streamClient()
    const controller = new WorkspaceController(client)
    const connect = controller.connect()

    channels[0].push(event({ sequence: 1 }))
    await tick()
    channels[0].push(
      event({ sequence: 2, generation: 2, operation: { id: 10, kind: 'mutation' } }),
    )
    await tick()
    expect(controller.state.operation).toEqual({ id: 10, kind: 'mutation' })

    channels[0].push(
      event({
        sequence: 3,
        generation: 2,
        operation: null,
        payload: snapshot([], { settledOperation: { id: 9, kind: 'mutation' } }),
      }),
    )
    await tick()
    expect(controller.state.operation).toEqual({ id: 10, kind: 'mutation' })
    controller.dispose()
    await connect
  })

  // spec: session-navigation/remote-marketplace-search-is-debounced
  // spec: session-navigation/clear-and-refresh
  it('rejects stale discovery frames and accepts only the current query without changing operation state', async () => {
    const { client, push } = fakeClient()
    const local = row('needle-local', { id: 'local-row', group: 'enabled' })
    const operation = { id: 7, kind: 'sync' as const }
    const { controller, wrapper } = mountWorkspace(client)
    push(event({
      sequence: 1,
      generation: 3,
      operation,
      payload: snapshot([local], { stage: 'config', drift: 'pending', operation, discoveryEnabled: true }),
    }))
    await tick()

    await wrapper.find('button[aria-label="Search"]').trigger('click')
    await tick()
    await wrapper.find('input[aria-label="Search resources"]').setValue('needle')
    await tick()

    const pushDiscovery = (sequence: number, generation: number, requestId: number, query: string, discoverySequence: number, name: string, label: string) => {
      push({
        type: 'discovery',
        sequence,
        generation,
        operation: null,
        payload: {
          requestId,
          query,
          discoverySequence,
          rows: [local, row(name, { id: name, discovery: true, group: 'available', checked: false, scope: 'available', status: 'available' })],
          health: [{ id: label, label, total: 1, truncated: false, cacheState: 'fresh', message: '' }],
          loading: false,
        },
      } as unknown as SessionEvent)
    }

    pushDiscovery(2, 3, 0, '', 1, 'remote-old-query', 'old query')
    pushDiscovery(3, 2, 1, 'needle', 2, 'remote-old-generation', 'old generation')
    pushDiscovery(4, 3, 0, 'needle', 2, 'remote-old-request', 'old request')
    pushDiscovery(5, 3, 1, 'other', 3, 'remote-wrong-query', 'wrong query')
    await tick()
    expect(controller.state.rows.map((entry) => entry.id)).toEqual(['local-row'])
    expect(controller.state.discoveryHealth).toEqual([])

    pushDiscovery(6, 3, 1, 'needle', 4, 'remote-current', 'current Marketplace')
    await tick()
    expect(controller.state.rows.map((entry) => entry.id)).toEqual(['local-row', 'remote-current'])
    expect(controller.state.stage).toBe('config')
    expect(controller.state.drift).toBe('pending')
    expect(controller.state.diagnostics).toEqual([])
    if (controller.state.discoveryQuery !== 'needle') {
      throw new Error('accepted discovery query was not retained')
    }
    expect(controller.state.discoveryRequestId).toBe(1)
    expect(controller.state.discoverySequence).toBe(4)
    expect(controller.state.discoveryHealth[0]?.label).toBe('current Marketplace')
    expect(wrapper.find('[role="option"][aria-selected="true"]').attributes('id')).toBe('row-local-row')
    expect(controller.state.operation).toEqual(operation)
    expect(client.telemetry).not.toHaveBeenCalled()

    pushDiscovery(7, 3, 1, 'needle', 3, 'remote-old-sequence', 'old sequence')
    await tick()
    expect(controller.state.rows.map((entry) => entry.id)).toEqual(['local-row', 'remote-current'])
    expect(controller.state.discoveryHealth[0]?.label).toBe('current Marketplace')
    expect(controller.state.operation).toEqual(operation)
  })

  // spec: session-navigation/remote-marketplace-search-is-debounced
  it('advances issuance beyond reconnection state without restoring its prior query', async () => {
    const { client, push } = fakeClient()
    const local = row('local-resource', { id: 'local-row', group: 'enabled' })
    const oldRemote = row('old-remote', { id: 'old-remote', discovery: true, group: 'available', checked: false, scope: 'available', status: 'available' })
    const restoredQuery = String.fromCharCode(111, 108, 100)
    const newQuery = String.fromCharCode(110, 101, 119)
    const { controller, wrapper } = mountWorkspace(client)
    push(event({
      sequence: 1,
      generation: 4,
      payload: snapshot([local, oldRemote], {
        stage: 'config',
        drift: 'pending',
        discoveryEnabled: true,
        discoveryHealth: [{ id: 'market', label: 'Marketplace', total: 1, truncated: false, cacheState: 'fresh', message: '' }],
        discoveryQuery: restoredQuery,
        discoveryRequestId: 7,
        discoverySequence: 3,
      }),
    }))
    await tick()
    expect(controller.state.discoveryQuery).toBe('')
    expect(controller.state.discoveryRequestId).toBe(7)
    expect(controller.state.rows.map((entry) => entry.id)).toEqual(['local-row'])

    await wrapper.find('button[aria-label="Search"]').trigger('click')
    await wrapper.find('input[aria-label="Search resources"]').setValue(newQuery)
    await tick()
    const discovery = client.discovery as unknown as ReturnType<typeof vi.fn>
    const calls = discovery.mock.calls as unknown as [string, number, AbortSignal?][]
    if (calls.length !== 1 || calls[0][0] !== newQuery || calls[0][1] !== 8) {
      throw new Error('new search did not advance beyond the server issuance high-water mark')
    }
  })

  // spec: session-startup/final-stage-merges-discovery-published-during-the-config-stage
  it('retains merged discovery through final assessment without action or quit settlement', async () => {
    const { client, push } = fakeClient()
    const local = row('local-resource', { id: 'local-row', group: 'enabled' })
    const remote = row('remote-resource', { id: 'remote-row', discovery: true, group: 'available', checked: false, scope: 'available', status: 'available' })
    const quit = vi.fn()
    const { controller, wrapper } = mountWorkspace(client, 60_000, quit)
    push(event({
      sequence: 1,
      generation: 4,
      payload: snapshot([local], { stage: 'config', drift: 'pending', discoveryEnabled: true }),
    }))
    await tick()
    push({
      type: 'discovery',
      sequence: 2,
      generation: 4,
      operation: null,
      payload: { requestId: 0, query: '', discoverySequence: 1, rows: [local, remote], health: [{ id: 'market', label: 'Marketplace', total: 1, truncated: false, cacheState: 'fresh', message: '' }], loading: false },
    } as SessionEvent)
    await tick()
    expect(controller.state.stage).toBe('config')
    expect(controller.state.drift).toBe('pending')
    expect(controller.state.rows.map((entry) => entry.id)).toEqual(['local-row', 'remote-row'])
    expect(controller.state.operation).toBeNull()

    push(event({
      sequence: 3,
      generation: 4,
      payload: snapshot([local, remote], {
        stage: 'final',
        drift: 'ready',
        discoveryEnabled: true,
        discoveryHealth: [{ id: 'market', label: 'Marketplace', total: 1, truncated: false, cacheState: 'fresh', message: '' }],
        discoveryQuery: '',
        discoveryRequestId: 0,
        discoverySequence: 1,
        discoveryLoading: false,
      }),
    }))
    await tick()
    expect(controller.state.stage).toBe('final')
    expect(controller.state.drift).toBe('ready')
    expect(controller.state.rows.map((entry) => entry.id)).toEqual(['local-row', 'remote-row'])
    expect(wrapper.find('[role="option"][id="row-remote-row"]').exists()).toBe(true)
    expect(client.action).not.toHaveBeenCalled()
    expect(client.telemetry).not.toHaveBeenCalled()
    expect(quit).not.toHaveBeenCalled()
  })

  // spec: session-rendering-safety/hostile-text-is-inert
  it('renders sanitized Marketplace health text literally without activating markup', async () => {
    const { client, push } = fakeClient()
    const label = 'market <img src=x onerror=alert(1)>\\\\x1B[31m\\\\u202E Å'
    const message = 'Marketplace search failed <script>\\\\x1B[31m\\\\u202E Ω'
    const { wrapper } = mountWorkspace(client)
    push(event({
      sequence: 1,
      payload: snapshot([], {
        discoveryEnabled: true,
        discoveryHealth: [{ id: 'health-id', label, total: 1, truncated: false, cacheState: 'stale', message }],
      }),
    }))
    await tick()

    const health = wrapper.get('[aria-label="Marketplace discovery health"]')
    expect(health.text()).toContain(label)
    expect(health.text()).toContain(message)
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('script').exists()).toBe(false)
  })

  // spec: session-navigation/remote-marketplace-search-is-debounced
  it('retries a failed discovery with a new request identity and preserves the current local view', async () => {
    const { client, push } = fakeClient()
    const query = String.fromCharCode(110, 101, 101, 100, 108, 101)
    const local = row('needle-local', { id: 'local-row', group: 'enabled' })
    const operation = { id: 7, kind: 'sync' as const }
    const quit = vi.fn()
    const { controller, wrapper } = mountWorkspace(client, 60_000, quit)
    push(event({
      sequence: 1,
      generation: 3,
      operation,
      payload: snapshot([local], { stage: 'config', drift: 'pending', operation, discoveryEnabled: true }),
    }))
    await tick()

    await wrapper.find('button[aria-label="Search"]').trigger('click')
    await wrapper.find('input[aria-label="Search resources"]').setValue(query)
    await tick()
    const discovery = client.discovery as unknown as ReturnType<typeof vi.fn>
    const firstCalls = discovery.mock.calls as unknown as [string, number, AbortSignal?][]
    if (firstCalls.length !== 1 || firstCalls[0][1] !== 1) {
      throw new Error('first discovery intent was not issued')
    }
    push({
      type: 'discovery',
      sequence: 2,
      generation: 3,
      operation: null,
      payload: {
        requestId: 1,
        query,
        discoverySequence: 1,
        rows: [local],
        health: [{ id: 'market', label: 'Marketplace', total: 1, truncated: false, cacheState: 'stale', message: 'Marketplace search failed; cached results were retained when available.' }],
        loading: false,
      },
    } as SessionEvent)
    await tick()
    const retry = wrapper.find('button[aria-label="Retry Marketplace search"]')
    expect(retry.exists()).toBe(true)
    await retry.trigger('click')
    await tick()

    const retryCalls = discovery.mock.calls as unknown as [string, number, AbortSignal?][]
    if (retryCalls.length !== 2 || retryCalls[1][0] !== query || retryCalls[1][1] !== 2) {
      throw new Error('retry did not issue the current query with a newer identity')
    }
    expect(retryCalls[0][2]?.aborted).toBe(true)
    expect(controller.state.rows.map((entry) => entry.id)).toEqual(['local-row'])
    expect(controller.state.operation).toEqual(operation)
    expect(quit).not.toHaveBeenCalled()

    push({
      type: 'discovery',
      sequence: 3,
      generation: 3,
      operation: null,
      payload: {
        requestId: 2,
        query,
        discoverySequence: 2,
        rows: [local, row('needle-remote', { id: 'remote-row', discovery: true, group: 'available', checked: false, scope: 'available', status: 'available' })],
        health: [{ id: 'market', label: 'Marketplace', total: 1, truncated: false, cacheState: 'fresh', message: '' }],
        loading: false,
      },
    } as SessionEvent)
    await tick()
    if (controller.state.discoveryQuery !== query) {
      throw new Error('retry changed the active query')
    }
    expect(controller.state.discoveryRequestId).toBe(2)
    expect(controller.state.rows.map((entry) => entry.id)).toEqual(['local-row', 'remote-row'])
    expect(wrapper.find('[role="option"][aria-selected="true"]').attributes('id')).toBe('row-local-row')
    expect(controller.state.operation).toEqual(operation)
    expect(client.action).not.toHaveBeenCalled()
    expect(quit).not.toHaveBeenCalled()

    controller.dispose()
    controller.retryDiscovery()
    push({
      type: 'discovery',
      sequence: 4,
      generation: 3,
      operation: null,
      payload: {
        requestId: 2,
        query,
        discoverySequence: 3,
        rows: [row('late-remote', { id: 'late-row', discovery: true })],
        health: [],
        loading: false,
      },
    } as SessionEvent)
    await tick()
    expect(discovery.mock.calls).toHaveLength(2)
    expect(controller.state.rows.map((entry) => entry.id)).toEqual(['local-row', 'remote-row'])
  })

  // spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
  it('opens verified inspection by keyboard, retries, navigates members, rejects stale replies, and closes without operations', async () => {
    type Pending = {
      rowId: string
      fileId?: string
      signal?: AbortSignal
      resolve: (value: import('./api/client').InspectionPayload) => void
      reject: (reason: unknown) => void
    }
    const { client, push } = fakeClient()
    const calls: Pending[] = []
    const inspection = vi.fn((rowId: string, fileId?: string, signal?: AbortSignal) =>
      new Promise<import('./api/client').InspectionPayload>((resolve, reject) => calls.push({ rowId, fileId, signal, resolve, reject })),
    )
    const inspectionClient = Object.assign(client, { inspection })
    const first = row('first', { id: 'row-first', discovery: true, inspectable: true, group: 'available', checked: false, scope: 'available', status: 'available' })
    const second = row('second', { id: 'row-second', discovery: true, inspectable: true, group: 'available', checked: false, scope: 'available', status: 'available' })
    const operation: OperationRef = { id: 91, kind: 'sync' }
    const quit = vi.fn()
    const { controller, wrapper } = mountWorkspace(inspectionClient, 60_000, quit)
    push(event({ sequence: 1, generation: 4, operation, payload: snapshot([first, second], { discoveryEnabled: true }) }))
    await tick()

    const initialState = { stage: controller.state.stage, drift: controller.state.drift, operation: controller.state.operation }
    const firstRow = wrapper.find('[role="option"][aria-selected="true"]')
    await firstRow.trigger('keydown', { key: 'Enter' })
    await tick()
    expect(inspection).toHaveBeenCalledTimes(1)
    expect(calls[0]?.rowId).toBe(first.id)
    expect(calls[0]?.fileId).toBeUndefined()
    const firstSignal = calls[0]?.signal
    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull()

    await firstRow.trigger('keydown', { key: 'ArrowDown' })
    await tick()
    expect(firstSignal?.aborted).toBe(true)
    expect(inspection).toHaveBeenCalledTimes(1)
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()

    const secondRow = wrapper.find('[role="option"][aria-selected="true"]')
    await secondRow.trigger('keydown', { key: 'Enter' })
    await tick()
    expect(calls[1]?.rowId).toBe(second.id)
    const failedSignal = calls[1]?.signal
    calls[1]?.reject(new RequestError(502, 'localapi.inspection_unavailable'))
    await tick()
    expect(document.body.querySelector('[role="alert"]')?.textContent).toContain('Verified inspection is unavailable')
    const retry = document.body.querySelector('button[aria-label="Retry inspection"]') as HTMLButtonElement | null
    expect(retry).not.toBeNull()
    retry?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await tick()
    expect(calls[2]?.signal).not.toBe(failedSignal)
    expect(calls[2]?.rowId).toBe(second.id)
    expect(calls[2]?.fileId).toBeUndefined()

    const primaryID = 'p'.repeat(64)
    const notesID = 'n'.repeat(64)
    const imageID = 'i'.repeat(64)
    const files: import('./api/client').InspectionPayload['files'] = [
      { id: primaryID, path: 'SKILL.md', mediaType: 'text/markdown', size: 64, inspectionClass: 'text' },
      { id: notesID, path: 'support/notes.md', mediaType: 'text/markdown', size: 24, inspectionClass: 'text' },
      { id: imageID, path: 'support/logo.png', mediaType: 'image/png', size: 8, inspectionClass: 'image' },
    ]
    const payload = (rowId: string, selectedFileId: string, content: string, unsupportedMessage = '') => ({
      rowId,
      generation: 4,
      revision: 'sha256:' + 'a'.repeat(64),
      manifestDigest: 'sha256:' + 'b'.repeat(64),
      files,
      primaryFileId: primaryID,
      selectedFileId,
      content,
      truncated: false,
      unsupportedMessage,
    })
    const hostile = 'Literal <script>alert(1)</script> <img src=x onerror=alert(1)> \u001b[31m\u202e'
    calls[2]?.resolve(payload(second.id, primaryID, hostile))
    await tick()
    expect(document.body.querySelector('script')).toBeNull()
    expect(document.body.querySelector('img')).toBeNull()
    expect(document.body.querySelector('pre')?.textContent).toContain('<script>alert(1)</script>')
    const primaryItem = document.body.querySelector('[role="treeitem"][aria-label="SKILL.md"]') as HTMLElement | null
    expect(primaryItem?.getAttribute('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(primaryItem)

    const supportDirectory = document.body.querySelector('[role="treeitem"][aria-label="support"]') as HTMLElement | null
    expect(supportDirectory).not.toBeNull()
    document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }))
    await tick()
    expect(document.activeElement).toBe(supportDirectory)
    supportDirectory?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await tick()
    supportDirectory?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await tick()
    const imageItem = document.body.querySelector('[role="treeitem"][aria-label="support/logo.png"]') as HTMLElement | null
    expect(document.activeElement).toBe(imageItem)
    imageItem?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }))
    await tick()
    expect(document.activeElement).toBe(supportDirectory)
    supportDirectory?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await tick()
    expect(supportDirectory?.getAttribute('aria-expanded')).toBe('false')
    supportDirectory?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await tick()
    expect(supportDirectory?.getAttribute('aria-expanded')).toBe('true')
    supportDirectory?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await tick()
    const expandedImage = document.body.querySelector('[role="treeitem"][aria-label="support/logo.png"]') as HTMLElement | null
    expect(document.activeElement).toBe(expandedImage)
    expandedImage?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    await tick()
    const notes = document.body.querySelector('[role="treeitem"][aria-label="support/notes.md"]') as HTMLElement | null
    expect(notes).not.toBeNull()
    expect(document.activeElement).toBe(notes)
    notes?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await tick()
    expect(calls[3]?.rowId).toBe(second.id)
    expect(calls[3]?.fileId).toBe(notesID)
    calls[3]?.resolve(payload(second.id, notesID, '# Supporting inspection.'))
    await tick()

    calls[0]?.resolve(payload(first.id, primaryID, 'stale first resource'))
    await tick()
    expect(controller.state.inspection?.rowId).toBe(second.id)
    expect(document.body.querySelector('pre')?.textContent).toBe('# Supporting inspection.')

    const image = document.body.querySelector('[role="treeitem"][aria-label="support/logo.png"]') as HTMLElement | null
    expect(image).not.toBeNull()
    document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }))
    await tick()
    expect(document.activeElement).toBe(image)
    image?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await tick()
    expect(calls[4]?.fileId).toBe(imageID)
    calls[4]?.resolve(payload(second.id, imageID, '', 'Preview is not supported for this file type (image/png, 8 bytes).'))
    await tick()
    expect(controller.state.inspection?.data?.unsupportedMessage).toContain('image/png')
    expect(document.body.querySelector('pre')?.textContent ?? '').toBe('')

    const notesAgain = document.body.querySelector('[role="treeitem"][aria-label="support/notes.md"]') as HTMLElement | null
    notesAgain?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await tick()
    expect(calls[5]?.fileId).toBe(notesID)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await tick()
    expect(calls[5]?.signal?.aborted).toBe(true)
    expect(controller.state.inspection).toBeNull()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect((document.activeElement as HTMLElement | null)?.getAttribute('aria-label')).toBe('Open resource')

    controller.dispose()
    controller.openInspection(second.id)
    calls[5]?.resolve(payload(second.id, notesID, 'late support body'))
    await tick()
    expect(inspection).toHaveBeenCalledTimes(6)
    expect(controller.state.inspection).toBeNull()
    expect(controller.state.stage).toBe(initialState.stage)
    expect(controller.state.drift).toBe(initialState.drift)
    expect(controller.state.operation).toEqual(initialState.operation)
    expect(client.action).not.toHaveBeenCalled()
    expect(client.configure).not.toHaveBeenCalled()
    for (const [event] of (client.telemetry as ReturnType<typeof vi.fn>).mock.calls) {
      expect(event).not.toHaveProperty('filePath')
      expect(event).not.toHaveProperty('path')
      expect(event).not.toHaveProperty('sourceName')
    }
    expect(quit).not.toHaveBeenCalled()
  })

  it('invalidates an inspection after a new core generation or a discovery result removes its row', async () => {
    type Pending = {
      rowId: string
      signal?: AbortSignal
      resolve: (value: import('./api/client').InspectionPayload) => void
    }
    const { client, push } = fakeClient()
    const calls: Pending[] = []
    const inspection = vi.fn((rowId: string, _fileId?: string, signal?: AbortSignal) =>
      new Promise<import('./api/client').InspectionPayload>((resolve) => calls.push({ rowId, signal, resolve })),
    )
    const inspectionClient = Object.assign(client, { inspection })
    const remote = row('remote', { id: 'remote-row', discovery: true, inspectable: true, group: 'available', checked: false, scope: 'available', status: 'available' })
    const { controller } = mountWorkspace(inspectionClient)
    push(event({ sequence: 1, generation: 1, payload: snapshot([remote], { discoveryEnabled: true }) }))
    await tick()

    controller.openInspection(remote.id)
    await tick()
    const firstSignal = calls[0]?.signal
    push(event({ sequence: 2, generation: 2, payload: snapshot([remote], { discoveryEnabled: true }) }))
    await tick()
    expect(firstSignal?.aborted).toBe(true)
    expect(controller.state.inspection).toBeNull()

    controller.openInspection(remote.id)
    await tick()
    const secondSignal = calls[1]?.signal
    controller.setDiscoveryQuery('no-match')
    push({
      type: 'discovery',
      sequence: 3,
      generation: 2,
      operation: null,
      payload: { requestId: 1, query: 'no-match', discoverySequence: 1, rows: [], health: [], loading: false },
    } as SessionEvent)
    await tick()
    expect(secondSignal?.aborted).toBe(true)
    expect(controller.state.inspection).toBeNull()

    const stalePayload = {
      rowId: remote.id,
      generation: 2,
      revision: 'sha256:' + 'a'.repeat(64),
      manifestDigest: 'sha256:' + 'b'.repeat(64),
      files: [],
      primaryFileId: 'p'.repeat(64),
      selectedFileId: 'p'.repeat(64),
      content: 'stale',
      truncated: false,
      unsupportedMessage: '',
    }
    calls[0]?.resolve({ ...stalePayload, generation: 1 })
    calls[1]?.resolve(stalePayload)
    await tick()
    expect(controller.state.inspection).toBeNull()
    controller.dispose()
  })

  // spec: session-navigation/installed-skill-definition-and-supporting-files-remain-browsable
  it('uses the installed-files RPC for keyboard browsing and ignores superseded responses', async () => {
    type Pending = {
      rowId: string
      fileId?: string
      signal?: AbortSignal
      resolve: (value: import('./api/client').InstalledSkillFilesPayload) => void
      reject: (reason: unknown) => void
    }
    const { client, push } = fakeClient()
    const calls: Pending[] = []
    const installedSkillFiles = vi.fn((rowId: string, fileId?: string, signal?: AbortSignal) =>
      new Promise<import('./api/client').InstalledSkillFilesPayload>((resolve, reject) => calls.push({ rowId, fileId, signal, resolve, reject })),
    )
    const testClient = Object.assign(client, { installedSkillFiles })
    const first = row('alpha', { id: 'row-alpha', installedFiles: true, group: 'enabled', checked: true, status: 'enabled' })
    const second = row('beta', { id: 'row-beta', installedFiles: true, group: 'enabled', checked: true, status: 'enabled' })
    const operation: OperationRef = { id: 91, kind: 'sync' }
    const quit = vi.fn()
    const { controller, wrapper } = mountWorkspace(testClient, 60_000, quit)
    push(event({ sequence: 1, generation: 4, operation, payload: snapshot([first, second]) }))
    await tick()
    const initial = { stage: controller.state.stage, drift: controller.state.drift, operation: controller.state.operation }

    const firstRow = wrapper.find('[role="option"][aria-selected="true"]')
    await firstRow.trigger('keydown', { key: 'Enter' })
    await tick()
    expect(installedSkillFiles).toHaveBeenCalledTimes(1)
    expect(calls[0]?.rowId).toBe(first.id)
    expect(calls[0]?.fileId).toBeUndefined()
    expect(client.inspection).not.toHaveBeenCalled()
    const failedSignal = calls[0]?.signal
    calls[0]?.reject(new RequestError(502, 'localapi.resource_files_unavailable'))
    await tick()
    expect(document.body.querySelector('[role="alert"]')?.textContent).toContain('Resource files are unavailable')

    const retry = document.body.querySelector('button[aria-label="Retry inspection"]') as HTMLButtonElement | null
    expect(retry).not.toBeNull()
    retry?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await tick()
    expect(calls[1]?.signal).not.toBe(failedSignal)
    expect(calls[1]?.rowId).toBe(first.id)
    expect(calls[1]?.fileId).toBeUndefined()

    const primaryID = 'p'.repeat(64)
    const notesID = 'n'.repeat(64)
    const scriptID = 's'.repeat(64)
    const files: import('./api/client').InstalledSkillFilesPayload['files'] = [
      { id: primaryID, path: 'SKILL.md', mediaType: 'text/markdown', size: 64, inspectionClass: 'text' },
      { id: notesID, path: 'notes.md', mediaType: 'text/markdown', size: 22, inspectionClass: 'text' },
      { id: scriptID, path: 'scripts/helper.sh', mediaType: 'text/plain', size: 17, inspectionClass: 'text' },
    ]
    const payload = (rowId: string, generation: number, selectedFileId: string, content: string) => ({
      rowId,
      generation,
      files,
      primaryFileId: primaryID,
      selectedFileId,
      content,
      truncated: false,
      unsupportedMessage: '',
    })
    calls[1]?.resolve(payload(first.id, 4, primaryID, '# Installed alpha'))
    await tick()
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog).not.toBeNull()
    const primaryItem = document.body.querySelector('[role="treeitem"][aria-label="SKILL.md"]') as HTMLElement | null
    expect(document.activeElement).toBe(primaryItem)
    expect(document.body.querySelector('pre')?.textContent).toBe('# Installed alpha')

    document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }))
    await tick()
    const notes = document.body.querySelector('[role="treeitem"][aria-label="notes.md"]') as HTMLElement | null
    expect(document.activeElement).toBe(notes)
    notes?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await tick()
    expect(calls[2]?.rowId).toBe(first.id)
    expect(calls[2]?.fileId).toBe(notesID)
    calls[2]?.resolve(payload(first.id, 4, notesID, '# Supporting review'))
    await tick()
    expect(document.body.querySelector('pre')?.textContent).toBe('# Supporting review')

    document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }))
    await tick()
    const scripts = document.body.querySelector('[role="treeitem"][aria-label="scripts"]') as HTMLElement | null
    expect(document.activeElement).toBe(scripts)
    scripts?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await tick()
    scripts?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await tick()
    const helper = document.body.querySelector('[role="treeitem"][aria-label="scripts/helper.sh"]') as HTMLElement | null
    expect(document.activeElement).toBe(helper)
    helper?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await tick()
    expect(calls[3]?.rowId).toBe(first.id)
    expect(calls[3]?.fileId).toBe(scriptID)
    calls[3]?.resolve(payload(first.id, 4, scriptID, 'printf "review"'))
    await tick()
    expect(document.body.querySelector('pre')?.textContent).toBe('printf "review"')
    expect(document.body.querySelector('script, img')).toBeNull()

    helper?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await tick()
    const supersededSignal = calls[4]?.signal
    push(event({ sequence: 2, generation: 5, operation, payload: snapshot([second]) }))
    await tick()
    expect(supersededSignal?.aborted).toBe(true)
    expect(controller.state.inspection).toBeNull()

    const betaRow = wrapper.find('[role="option"][aria-selected="true"]')
    await betaRow.trigger('keydown', { key: 'Enter' })
    await tick()
    expect(calls[5]?.rowId).toBe(second.id)
    calls[5]?.resolve(payload(second.id, 5, primaryID, '# Installed beta'))
    await tick()
    calls[4]?.resolve(payload(first.id, 4, scriptID, 'stale alpha script'))
    await tick()
    expect(controller.state.inspection?.rowId).toBe(second.id)
    expect(document.body.querySelector('pre')?.textContent).toBe('# Installed beta')

    document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }))
    await tick()
    const betaNotes = document.body.querySelector('[role="treeitem"][aria-label="notes.md"]') as HTMLElement | null
    betaNotes?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await tick()
    expect(calls[6]?.rowId).toBe(second.id)
    expect(calls[6]?.fileId).toBe(notesID)
    const closeSignal = calls[6]?.signal

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await tick()
    expect(closeSignal?.aborted).toBe(true)
    expect(controller.state.inspection).toBeNull()
    expect((document.activeElement as HTMLElement | null)?.getAttribute('aria-label')).toBe('Open resource')
    expect(controller.state.stage).toBe(initial.stage)
    expect(controller.state.drift).toBe(initial.drift)
    expect(controller.state.operation).toEqual(initial.operation)
    expect(client.action).not.toHaveBeenCalled()
    expect(client.configure).not.toHaveBeenCalled()
    expect(client.inspection).not.toHaveBeenCalled()
    controller.dispose()
    calls[6]?.resolve(payload(second.id, 5, notesID, 'late disposed response'))
    await tick()
    expect(controller.state.inspection).toBeNull()
    expect(installedSkillFiles).toHaveBeenCalledTimes(7)
    for (const [event] of (client.telemetry as ReturnType<typeof vi.fn>).mock.calls) {
      expect(event).not.toHaveProperty('filePath')
      expect(event).not.toHaveProperty('path')
      expect(event).not.toHaveProperty('sourceName')
    }
    expect(quit).not.toHaveBeenCalled()
  })
})
