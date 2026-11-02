// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { fakeClient, mountWorkspace, releaseNotice, row, snapshot, tick, event } from './test-support'

type UpdateAction = 'start' | 'status' | 'confirm' | 'cancel'
type UpdatePhase =
  | 'idle'
  | 'downloading'
  | 'verifying'
  | 'ready'
  | 'swapping'
  | 'handing-off'
  | 'complete'
  | 'fallback'
  | 'failed'
  | 'cancelled'
type UpdateStatus = {
  phase: UpdatePhase
  progress: number
  error: string
  browserURL: string
  releasePageURL: string
  savedPath: string
  moveToApplications: boolean
  restartRequired: boolean
}

const releasePageURL = 'https://github.com/Adyen/agent-marketplace/releases/tag/v2.0.0'

function updateStatus(
  phase: UpdatePhase,
  overrides: Partial<UpdateStatus> = {},
): UpdateStatus {
  return {
    phase,
    progress: 0,
    error: '',
    browserURL: '',
    releasePageURL,
    savedPath: '',
    moveToApplications: false,
    restartRequired: false,
    ...overrides,
  }
}

function releaseWithAsset() {
  return {
    ...releaseNotice('v2.0.0'),
    releasePageURL,
    assetURL: 'https://github.com/Adyen/agent-marketplace/releases/download/v2.0.0/gui.zip',
    checksumsURL: 'https://github.com/Adyen/agent-marketplace/releases/download/v2.0.0/SHA256SUMS',
  }
}

function withUpdate(client: ReturnType<typeof fakeClient>['client'], handler: (action: UpdateAction) => Promise<UpdateStatus>) {
  return Object.assign(client, { update: vi.fn(handler) })
}

function publishPendingChanges(push: ReturnType<typeof fakeClient>['push']) {
  push(event({ sequence: 1, payload: snapshot([row('solo')], { pendingChanges: 2 }) }))
}

// spec: gui-shell/unpackaged-build-links-to-the-release-page
describe('GUI update notice', () => {
  it('offers only the release page for an unpackaged build', async () => {
    const { client } = fakeClient()
    ;(client.release as ReturnType<typeof vi.fn>).mockResolvedValue(releaseWithAsset())
    const { wrapper } = mountWorkspace(client)

    await tick()

    expect(wrapper.get('a[aria-label="View release"]').attributes('href')).toBe(releasePageURL)
    expect(wrapper.find('button[aria-label="Update now"]').exists()).toBe(false)
  })

  // spec: gui-shell/pending-work-requires-confirmation-before-the-update
  it('waits for the existing pending-work quit confirmation before confirming an update', async () => {
    const { client, push } = fakeClient()
    publishPendingChanges(push)
    ;(client.release as ReturnType<typeof vi.fn>).mockResolvedValue(releaseWithAsset())
    const update = withUpdate(client, async (action) => {
      if (action === 'start') return updateStatus('ready', { progress: 100 })
      if (action === 'confirm') return updateStatus('swapping', { progress: 100 })
      return updateStatus('ready', { progress: 100 })
    }).update
    const onQuit = vi.fn()
    const { wrapper } = mountWorkspace(client, 60_000, onQuit, 'macos-app')

    await tick()
    await wrapper.get('button[aria-label="Update now"]').trigger('click')
    await tick()

    expect(update).toHaveBeenCalledWith('start')
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain('change(s) not synced')
    expect(update).not.toHaveBeenCalledWith('confirm')

    document.body.querySelector<HTMLButtonElement>('[role="dialog"] button[aria-label="Quit without syncing"]')!.click()
    await tick()

    expect(update).toHaveBeenCalledWith('confirm')
    expect(onQuit).not.toHaveBeenCalled()
  })

  // spec: gui-shell/pending-work-requires-confirmation-before-the-update
  it('does not confirm or replace an update when the pending-work confirmation is cancelled', async () => {
    const { client, push } = fakeClient()
    publishPendingChanges(push)
    ;(client.release as ReturnType<typeof vi.fn>).mockResolvedValue(releaseWithAsset())
    const update = withUpdate(client, async (action) => {
      if (action === 'start') return updateStatus('ready', { progress: 100 })
      if (action === 'cancel') return updateStatus('cancelled')
      return updateStatus('ready', { progress: 100 })
    }).update
    const { wrapper } = mountWorkspace(client, 60_000, undefined, 'macos-app')

    await tick()
    await wrapper.get('button[aria-label="Update now"]').trigger('click')
    await tick()
    document.body.querySelector<HTMLButtonElement>('[role="dialog"] button[aria-label="Cancel quit"]')!.click()
    await tick()

    expect(update.mock.calls.map(([action]) => action)).toEqual(['start', 'cancel'])
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
  })

  // spec: gui-shell/corrupted-update-download-is-rejected
  it('shows an update error and allows a retry', async () => {
    const { client } = fakeClient()
    ;(client.release as ReturnType<typeof vi.fn>).mockResolvedValue(releaseWithAsset())
    const update = withUpdate(client, async (action) =>
      action === 'start'
        ? updateStatus('failed', { error: 'The update could not be verified.' })
        : updateStatus('downloading', { progress: 32 }),
    ).update
    const { wrapper } = mountWorkspace(client, 60_000, undefined, 'macos-app')

    await tick()
    await wrapper.get('button[aria-label="Update now"]').trigger('click')
    await tick()

    expect(wrapper.text()).toContain('The update could not be completed.')
    await wrapper.get('button[aria-label="Try update again"]').trigger('click')
    await tick()
    expect(update.mock.calls.filter(([action]) => action === 'start')).toHaveLength(2)
  })

  // spec: gui-shell/ineligible-location-falls-back-to-a-browser-download
  it('offers the release disk image and Applications hint for an ineligible macOS location', async () => {
    const { client } = fakeClient()
    ;(client.release as ReturnType<typeof vi.fn>).mockResolvedValue(releaseWithAsset())
    withUpdate(client, async () =>
      updateStatus('fallback', {
        browserURL: 'https://github.com/Adyen/agent-marketplace/releases/download/v2.0.0/gui.dmg',
        moveToApplications: true,
      }),
    )
    const { wrapper } = mountWorkspace(client, 60_000, undefined, 'macos-app')

    await tick()
    await wrapper.get('button[aria-label="Update now"]').trigger('click')
    await tick()

    expect(wrapper.get('a[aria-label="Download disk image"]').attributes('href')).toContain('.dmg')
    expect(wrapper.text()).toContain('Move the app to Applications to enable updates.')
  })

  // spec: gui-shell/user-hands-the-verified-package-to-the-software-installer
  it('shows the Linux restart hint after handing the package to the installer', async () => {
    const { client } = fakeClient()
    ;(client.release as ReturnType<typeof vi.fn>).mockResolvedValue(releaseWithAsset())
    withUpdate(client, async () =>
      updateStatus('complete', {
        progress: 100,
        savedPath: '/home/alex/Downloads/agent-marketplace-gui.rpm',
        restartRequired: true,
      }),
    )
    const { wrapper } = mountWorkspace(client, 60_000, undefined, 'rpm')

    await tick()
    await wrapper.get('button[aria-label="Update now"]').trigger('click')
    await tick()

    expect(wrapper.text()).toContain('Install the package, then restart Agent Marketplace.')
  })

  it('shows download progress while an update is running', async () => {
    const { client } = fakeClient()
    ;(client.release as ReturnType<typeof vi.fn>).mockResolvedValue(releaseWithAsset())
    withUpdate(client, async () => updateStatus('downloading', { progress: 37 }))
    const { wrapper } = mountWorkspace(client, 60_000, undefined, 'rpm')

    await tick()
    await wrapper.get('button[aria-label="Update now"]').trigger('click')
    await tick()

    expect(wrapper.text()).toContain('Downloading update')
    expect(wrapper.get('progress').attributes('value')).toBe('37')
  })
})
