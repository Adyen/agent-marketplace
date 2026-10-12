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

  // spec: gui-shell/gui-actions-use-existing-telemetry-policy
  it('reports one canonical browse event for each click, Enter, and Space activation', async () => {
    const { client, push } = fakeClient()
    const selected = row('solo')
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([selected]) }))
    await tick()

    const option = wrapper.find('[role="option"]')
    await option.trigger('click')
    await option.trigger('keydown', { key: 'Enter' })
    await option.trigger('keydown', { key: ' ' })
    await tick()

    expect(client.telemetry).toHaveBeenCalledTimes(3)
    expect(client.telemetry).toHaveBeenNthCalledWith(1, { action: 'browse', rowId: selected.id })
    expect(client.telemetry).toHaveBeenNthCalledWith(2, { action: 'browse', rowId: selected.id })
    expect(client.telemetry).toHaveBeenNthCalledWith(3, { action: 'browse', rowId: selected.id })
    expect(option.attributes('aria-selected')).toBe('true')
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
    await options[0].trigger('click')
    await options[0].trigger('keydown', { key: 'ArrowDown' })
    await tick()
    await wrapper.findAll('[role="option"]')[1].trigger('keydown', { key: 'ArrowDown' })
    await tick()

    expect(client.telemetry).toHaveBeenCalledTimes(2)
    expect(client.telemetry).toHaveBeenNthCalledWith(1, { action: 'browse', rowId: first.id })
    expect(client.telemetry).toHaveBeenNthCalledWith(2, { action: 'browse', rowId: second.id })
    expect(wrapper.findAll('[role="option"]')[1].attributes('aria-selected')).toBe('true')
  })

  // spec: gui-shell/gui-actions-use-existing-telemetry-policy
  it('keeps the view and selected Resource unchanged when browse telemetry fails', async () => {
    const { client, push } = fakeClient()
    client.telemetry = vi.fn(async () => {
      throw new Error('telemetry unavailable')
    })
    const selected = row('solo')
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([selected]) }))
    await tick()

    await wrapper.find('[role="option"]').trigger('keydown', { key: 'Enter' })
    await tick()

    expect(client.telemetry).toHaveBeenCalledWith({ action: 'browse', rowId: selected.id })
    expect(wrapper.find('[role="option"]').attributes('aria-selected')).toBe('true')
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
})
