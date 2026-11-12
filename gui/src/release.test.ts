// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { event, fakeClient, mountWorkspace, releaseNotice, row, rowNames, snapshot, tick } from './test-support'
import type { ReleaseNotice } from './api/client'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

// spec: gui-shell/new-stable-release-is-available
describe('release notice', () => {
  it('shows the availability notice only for a newer reported release', async () => {
    const { client } = fakeClient()
    ;(client.release as ReturnType<typeof vi.fn>).mockResolvedValue(releaseNotice('v9.9.9'))
    const { controller, wrapper } = mountWorkspace(client)
    await tick()
    expect(wrapper.text()).toContain('Agent Marketplace v9.9.9 is available.')
    expect(controller.state.latestVersion).toBe('v9.9.9')
    expect(controller.state.error).toBeNull()
  })

  it('omits the notice for an empty latest version without error', async () => {
    const { client } = fakeClient()
    ;(client.release as ReturnType<typeof vi.fn>).mockResolvedValue(releaseNotice(''))
    const { controller, wrapper } = mountWorkspace(client)
    await tick()
    expect(wrapper.text()).not.toContain('is available.')
    expect(controller.state.latestVersion).toBe('')
    expect(controller.state.error).toBeNull()
  })

  // spec: gui-shell/version-check-fails
  it('stays silent when the release check fails', async () => {
    const { client } = fakeClient()
    ;(client.release as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('unreachable'))
    const { controller, wrapper } = mountWorkspace(client)
    await tick()
    expect(wrapper.text()).not.toContain('is available.')
    expect(controller.state.latestVersion).toBe('')
    expect(controller.state.error).toBeNull()
  })

  // spec: gui-shell/organization-disables-release-checking
  it('clears a prior notice when a refresh reports no newer release and keeps rows and selection', async () => {
    const { client, push } = fakeClient()
    const releaseMock = client.release as ReturnType<typeof vi.fn>
    releaseMock.mockResolvedValue(releaseNotice('v9.9.9'))
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('kept')]) }))
    await tick()
    expect(wrapper.text()).toContain('Agent Marketplace v9.9.9 is available.')
    const rowId = controller.state.rows[0].id
    const option = wrapper.find(`#row-${CSS.escape(rowId)}`)
    await option.trigger('click')
    expect(controller.state.error).toBeNull()

    releaseMock.mockResolvedValue(releaseNotice(''))
    await controller.refresh()
    await tick()
    expect(wrapper.text()).not.toContain('is available.')
    expect(controller.state.latestVersion).toBe('')
    expect(rowNames(wrapper)).toContain('kept')
    expect(wrapper.find(`#row-${CSS.escape(rowId)}`).attributes('aria-selected')).toBe('true')
    expect(controller.state.error).toBeNull()
  })

  // spec: gui-shell/organization-disables-release-checking
  it('a delayed earlier enabled response cannot re-arm a notice cleared by a later check', async () => {
    const { client } = fakeClient()
    const releaseMock = client.release as ReturnType<typeof vi.fn>
    const first = deferred<ReleaseNotice>()
    releaseMock.mockReturnValueOnce(first.promise)
    const { controller } = mountWorkspace(client)
    await tick()

    releaseMock.mockResolvedValue(releaseNotice(''))
    await controller.refresh()
    await tick()
    expect(controller.state.latestVersion).toBe('')

    first.resolve(releaseNotice('v9.9.9'))
    await tick()
    expect(controller.state.latestVersion).toBe('')
  })

  it('invalidates a pending release check on dispose', async () => {
    const { client } = fakeClient()
    const releaseMock = client.release as ReturnType<typeof vi.fn>
    const pending = deferred<ReleaseNotice>()
    releaseMock.mockReturnValue(pending.promise)
    const { controller } = mountWorkspace(client)
    await tick()
    controller.dispose()
    pending.resolve(releaseNotice('v9.9.9'))
    await tick()
    expect(controller.state.latestVersion).toBe('')
  })
})
