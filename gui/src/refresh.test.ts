// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RequestError, type Snapshot } from './api/client'
import { configuration, event, fakeClient, mountWorkspace, row, snapshot, tick } from './test-support'

const originalRuntime = window.runtime

afterEach(() => {
  window.runtime = originalRuntime
})

// spec: gui-shell/refresh-and-focus-reload-use-ordinary-snapshot-behavior
describe('workspace refresh', () => {
  it('explicit refresh reloads the configuration projection', async () => {
    const { client, push } = fakeClient()
    const { controller } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('a')]) }))
    await tick()
    const calls = (client.configuration as ReturnType<typeof vi.fn>).mock.calls.length

    ;(client.configuration as ReturnType<typeof vi.fn>).mockResolvedValue(
      configuration({ userTargets: ['claude'] }),
    )
    await controller.refresh()
    await tick()

    expect((client.snapshot as ReturnType<typeof vi.fn>)).toHaveBeenCalledTimes(1)
    expect((client.configuration as ReturnType<typeof vi.fn>).mock.calls.length).toBe(calls + 1)
    expect(controller.state.configuration?.userTargets).toEqual(['claude'])
  })

  it('keeps accepted rows through provisional, config, and inventory snapshots until final', async () => {
    const { client, push } = fakeClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('accepted')]) }))
    await tick()
    expect(wrapper.text()).toContain('skill/accepted')

    for (const [index, stage] of ['provisional', 'config', 'inventory'].entries()) {
      push(
        event({
          sequence: 2 + index,
          generation: 2,
          payload: snapshot([row('reloading')], { stage: stage as Snapshot['stage'] }),
        }),
      )
      await tick()
      expect(wrapper.text()).toContain('skill/accepted')
      expect(wrapper.text()).not.toContain('skill/reloading')
    }

    push(event({ sequence: 5, generation: 2, payload: snapshot([row('reloaded')]) }))
    await tick()
    expect(wrapper.text()).toContain('skill/reloaded')
    expect(wrapper.text()).not.toContain('skill/accepted')
    expect(controller.state.stage).toBe('final')
  })

  it('rejected snapshot keeps accepted rows and configuration while reporting the refresh error', async () => {
    const { client, push } = fakeClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('a')]) }))
    await tick()
    const loaded = controller.state.configuration

    ;(client.snapshot as ReturnType<typeof vi.fn>).mockRejectedValue(new RequestError(503, 'localapi.closed'))
    await controller.refresh()
    await tick()

    expect(controller.state.error).toBe('Refresh failed')
    expect(wrapper.text()).toContain('skill/a')
    expect(controller.state.configuration).toBe(loaded)
  })

  it('a failed configuration read preserves the previous projection', async () => {
    const { client, push } = fakeClient()
    const { controller } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('a')]) }))
    await tick()
    const loaded = controller.state.configuration
    expect(loaded?.userTargets).toEqual(['devin'])

    const calls = (client.configuration as ReturnType<typeof vi.fn>).mock.calls.length
    ;(client.configuration as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new RequestError(500, 'localapi.closed'))
    await controller.refresh()
    await tick()

    expect((client.configuration as ReturnType<typeof vi.fn>).mock.calls.length).toBe(calls + 1)
    expect(controller.state.configuration).toBe(loaded)
    expect(controller.state.error).toBeNull()
  })
})

// spec: gui-shell/external-file-changes-require-a-refresh-trigger
// spec: gui-shell/second-launch-focuses-the-running-instance
describe('focus and launch-request triggers', () => {
  function stubRuntime() {
    const callbacks: ((...data: unknown[]) => void)[] = []
    const unsubscribe = vi.fn(() => {
      callbacks.length = 0
    })
    const eventsOn = vi.fn((event: string, callback: (...data: unknown[]) => void) => {
      if (event === 'desktop:launch-request') {
        callbacks.push(callback)
      }
      return unsubscribe
    })
    window.runtime = { EventsOn: eventsOn }
    return { callbacks, eventsOn, unsubscribe }
  }

  it('window focus and launch-request both trigger the same refresh and unmount detaches both', async () => {
    const { client, push } = fakeClient()
    const { callbacks, eventsOn, unsubscribe } = stubRuntime()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('a')]) }))
    await tick()

    const snapshotMock = client.snapshot as ReturnType<typeof vi.fn>
    window.dispatchEvent(new window.Event('focus'))
    await tick()
    expect(snapshotMock).toHaveBeenCalledTimes(1)

    expect(callbacks).toHaveLength(1)
    callbacks[0]('agent-marketplace-gui')
    await tick()
    expect(snapshotMock).toHaveBeenCalledTimes(2)
    expect(eventsOn).toHaveBeenCalledWith('desktop:launch-request', expect.any(Function))

    wrapper.unmount()
    expect(unsubscribe).toHaveBeenCalledTimes(1)
    window.dispatchEvent(new window.Event('focus'))
    await tick()
    expect(snapshotMock).toHaveBeenCalledTimes(2)
  })

  it('a second trigger while a refresh request is pending does not start another snapshot', async () => {
    const { client, push } = fakeClient()
    const { callbacks } = stubRuntime()
    const { controller } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('a')]) }))
    await tick()

    const snapshotMock = client.snapshot as ReturnType<typeof vi.fn>
    const releases: ((value: Snapshot) => void)[] = []
    snapshotMock.mockImplementation(
      () => new Promise<Snapshot>((resolve) => releases.push(resolve)),
    )

    window.dispatchEvent(new window.Event('focus'))
    window.dispatchEvent(new window.Event('focus'))
    callbacks[0]('agent-marketplace-gui')
    await tick()
    expect(snapshotMock).toHaveBeenCalledTimes(1)

    releases[0](snapshot([row('a')]))
    await tick()

    window.dispatchEvent(new window.Event('focus'))
    await tick()
    expect(snapshotMock).toHaveBeenCalledTimes(2)

    releases[1](snapshot([row('a')]))
    await tick()
    expect(controller.state.requesting).toBe(false)
  })
})
