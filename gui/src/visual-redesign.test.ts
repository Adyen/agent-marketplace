// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { DOMWrapper } from '@vue/test-utils'
import type { OperationRef, ResourceActionPlan, Snapshot } from './api/client'
import { event, fakeClient, mountWorkspace, row, snapshot, tick } from './test-support'

function withAvailableUpdates(rows: ReturnType<typeof row>[], availableUpdates: number): Snapshot {
  return { ...snapshot(rows), availableUpdates } as Snapshot
}

async function dragHandle(handle: Pick<DOMWrapper<Element>, 'trigger'>, from: number, to: number) {
  await handle.trigger('pointerdown', { clientX: from, button: 0, pointerId: 1 })
  await handle.trigger('pointermove', { clientX: to, pointerId: 1 })
  await handle.trigger('pointerup', { clientX: to, pointerId: 1 })
}

function resourcePlan(rowId: string): ResourceActionPlan {
  return {
    rowId,
    generation: 1,
    planId: 'a'.repeat(64),
    action: 'install',
    preview: 'Resource: skill/review\nLayer: user',
    requiresRiskAcceptance: false,
  }
}

async function mountView(rows = [row('solo')], availableUpdates = 0) {
  const { client, push } = fakeClient()
  const mounted = mountWorkspace(client)
  push(event({ sequence: 1, payload: withAvailableUpdates(rows, availableUpdates) }))
  await tick()
  return { ...mounted, client }
}

beforeEach(() => {
  window.localStorage.clear()
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1400, writable: true })
})

describe('update availability toolbar', () => {
  // spec: gui-shell/update-shows-the-available-update-count
  it('shows the available update count before Sync without changing accessible names', async () => {
    const { wrapper } = await mountView([row('solo')], 3)
    const toolbar = wrapper.get('[role="toolbar"][aria-label="Session actions"]')
    const update = toolbar.get('button[aria-label="Update"]')
    const sync = toolbar.get('button[aria-label="Sync"]')
    const buttons = Array.from(toolbar.element.querySelectorAll('button'))

    expect(buttons.indexOf(update.element as HTMLButtonElement)).toBeLessThan(buttons.indexOf(sync.element as HTMLButtonElement))
    expect(update.text()).toContain('3')
    expect(update.attributes('aria-label')).toBe('Update')
    expect(sync.attributes('aria-label')).toBe('Sync')
  })

  // spec: gui-shell/no-update-indicator-at-zero-before-a-check-or-after-a-failed-check
  it('keeps the update indicator absent at zero and shows no failed-check error', async () => {
    const { wrapper } = await mountView([row('solo')], 0)
    const update = wrapper.get('button[aria-label="Update"]')

    expect(update.find('.gui-update-count').exists()).toBe(false)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
})

describe('resizable panes', () => {
  // spec: gui-shell/pointer-drag-resizes-and-clamps-a-pane
  it('resizes both panes by pointer and clamps each to its limits', async () => {
    const { wrapper, client } = await mountView()
    const sidebar = wrapper.get('[role="separator"][aria-label="Resize sidebar"]')
    const inspector = wrapper.get('[role="separator"][aria-label="Resize inspector"]')
    const snapshotCalls = vi.mocked(client.snapshot).mock.calls.length

    await dragHandle(sidebar, 260, 310)
    expect(sidebar.attributes('aria-valuenow')).toBe('310')
    await dragHandle(sidebar, 310, 500)
    expect(sidebar.attributes('aria-valuenow')).toBe('400')
    await dragHandle(sidebar, 400, 80)
    expect(sidebar.attributes('aria-valuenow')).toBe('200')

    await dragHandle(inspector, 1040, 1000)
    expect(inspector.attributes('aria-valuenow')).toBe('400')
    await dragHandle(inspector, 1000, 700)
    expect(inspector.attributes('aria-valuenow')).toBe('560')
    await dragHandle(inspector, 700, 1300)
    expect(inspector.attributes('aria-valuenow')).toBe('280')
    expect(client.snapshot).toHaveBeenCalledTimes(snapshotCalls)
  })

  // spec: gui-shell/keyboard-resizes-a-pane-from-its-handle
  it('resizes each pane with arrows, Home, and End and exposes its range', async () => {
    const { wrapper } = await mountView()
    const sidebar = wrapper.get('[role="separator"][aria-label="Resize sidebar"]')
    const inspector = wrapper.get('[role="separator"][aria-label="Resize inspector"]')

    expect(sidebar.attributes('aria-valuemin')).toBe('200')
    expect(sidebar.attributes('aria-valuemax')).toBe('400')
    expect(inspector.attributes('aria-valuemin')).toBe('280')
    expect(inspector.attributes('aria-valuemax')).toBe('560')
    await sidebar.trigger('keydown', { key: 'ArrowRight' })
    expect(sidebar.attributes('aria-valuenow')).toBe('276')
    await sidebar.trigger('keydown', { key: 'Home' })
    expect(sidebar.attributes('aria-valuenow')).toBe('200')
    await sidebar.trigger('keydown', { key: 'End' })
    expect(sidebar.attributes('aria-valuenow')).toBe('400')
    await inspector.trigger('keydown', { key: 'ArrowLeft' })
    expect(inspector.attributes('aria-valuenow')).toBe('376')
    await inspector.trigger('keydown', { key: 'Home' })
    expect(inspector.attributes('aria-valuenow')).toBe('280')
    await inspector.trigger('keydown', { key: 'End' })
    expect(inspector.attributes('aria-valuenow')).toBe('560')
  })

  // spec: gui-shell/pane-widths-persist-across-relaunch
  it('restores both chosen widths after a new workspace mounts', async () => {
    const first = await mountView()
    await first.wrapper.get('[role="separator"][aria-label="Resize sidebar"]').trigger('keydown', { key: 'ArrowRight' })
    await first.wrapper.get('[role="separator"][aria-label="Resize inspector"]').trigger('keydown', { key: 'ArrowLeft' })
    expect(window.localStorage.getItem('am-gui-sidebar-width')).toBe('276')
    expect(window.localStorage.getItem('am-gui-inspector-width')).toBe('376')
    const reopened = await mountView()
    expect(reopened.wrapper.get('[role="separator"][aria-label="Resize sidebar"]').attributes('aria-valuenow')).toBe('276')
    expect(reopened.wrapper.get('[role="separator"][aria-label="Resize inspector"]').attributes('aria-valuenow')).toBe('376')
  })

  // spec: gui-shell/missing-or-malformed-stored-pane-width-uses-the-default
  it('uses the pane defaults when stored widths are malformed, missing, or unavailable', async () => {
    window.localStorage.setItem('am-gui-sidebar-width', 'narrow')
    window.localStorage.setItem('am-gui-inspector-width', 'NaN')
    const malformed = await mountView()
    expect(malformed.wrapper.get('[role="separator"][aria-label="Resize sidebar"]').attributes('aria-valuenow')).toBe('260')
    expect(malformed.wrapper.get('[role="separator"][aria-label="Resize inspector"]').attributes('aria-valuenow')).toBe('360')
    window.localStorage.clear()
    const missing = await mountView()
    expect(missing.wrapper.get('[role="separator"][aria-label="Resize sidebar"]').attributes('aria-valuenow')).toBe('260')
    expect(missing.wrapper.get('[role="separator"][aria-label="Resize inspector"]').attributes('aria-valuenow')).toBe('360')
  })

  // spec: gui-shell/missing-or-malformed-stored-pane-width-uses-the-default
  it('uses pane defaults when browser storage is unavailable', async () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('storage is blocked')
    })
    const unavailable = await mountView()
    expect(unavailable.wrapper.get('[role="separator"][aria-label="Resize sidebar"]').attributes('aria-valuenow')).toBe('260')
    expect(unavailable.wrapper.get('[role="separator"][aria-label="Resize inspector"]').attributes('aria-valuenow')).toBe('360')
    getItem.mockRestore()
  })
})

describe('resource action dialog', () => {
  // spec: gui-shell/resource-action-dialog-uses-plain-language
  it('uses plain action copy and a collapsed technical-details disclosure without a header icon', async () => {
    const resource = row('review', {
      id: 'review-row',
      displayName: 'Review',
      registry: 'team tools',
      scope: 'available',
      group: 'available',
      status: 'available',
      checked: false,
      resourceAction: 'install',
    })
    const { client, push } = fakeClient()
    const mounted = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([resource]) }))
    vi.mocked(client.resourcePlan).mockResolvedValue(resourcePlan(resource.id))
    await tick()
    await mounted.wrapper.get('[role="option"]').trigger('click')
    await tick()
    await mounted.wrapper.get('button[aria-label="Install Resource"]').trigger('click')
    await tick()

    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog).not.toBeNull()
    if (dialog == null) throw new Error('install dialog was not rendered')
    expect(dialog.querySelector('.gui-modal__tile')).toBeNull()
    const copy = dialog.querySelector('.resource-actions__copy')
    expect(copy?.textContent).toContain('Install Review from team tools.')
    expect(copy?.textContent).toContain('After the next sync, it will be available in Devin.')
    expect(copy?.textContent).not.toMatch(/\b(config|configuration|lock|lockfile|harness|acquisition|materialize)\b/i)
    const details = dialog.querySelector('details')
    expect(details?.open).toBe(false)
    expect(details?.querySelector('summary')?.textContent).toBe('Technical details')
    expect(details?.querySelector('pre')?.textContent).toBe(resourcePlan(resource.id).preview)
  })

  // spec: gui-shell/spinner-shows-only-while-the-action-is-in-flight
  it('shows progress while preparing and submitting, then hides it after failure', async () => {
    let resolvePlan!: (plan: ResourceActionPlan) => void
    let rejectAction!: (error: Error) => void
    const resource = row('review', {
      id: 'review-row',
      scope: 'available',
      group: 'available',
      status: 'available',
      checked: false,
      resourceAction: 'install',
    })
    const { client, push } = fakeClient()
    const mounted = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([resource]) }))
    vi.mocked(client.resourcePlan).mockReturnValue(new Promise<ResourceActionPlan>((resolve) => { resolvePlan = resolve }))
    vi.mocked(client.resourceAction).mockReturnValue(new Promise<OperationRef>((_resolve, reject) => { rejectAction = reject }))
    await tick()
    await mounted.wrapper.get('[role="option"]').trigger('click')
    await tick()
    await mounted.wrapper.get('button[aria-label="Install Resource"]').trigger('click')
    await tick()
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog?.querySelector('.resource-actions__spinner')).not.toBeNull()

    resolvePlan(resourcePlan(resource.id))
    await tick()
    expect(dialog?.querySelector('.resource-actions__spinner')).toBeNull()
    ;(dialog?.querySelector('button[aria-label="Confirm install"]') as HTMLButtonElement).click()
    await tick()
    expect(dialog?.querySelector('.resource-actions__spinner')).not.toBeNull()

    rejectAction(new Error('request failed'))
    await tick()
    expect(dialog?.querySelector('.resource-actions__spinner')).toBeNull()
  })

  // spec: gui-shell/spinner-shows-only-while-the-action-is-in-flight
  it('hides progress after the confirmed action succeeds', async () => {
    let resolveAction!: (operation: OperationRef) => void
    const resource = row('review', {
      id: 'review-row',
      scope: 'available',
      group: 'available',
      status: 'available',
      checked: false,
      resourceAction: 'install',
    })
    const { client, push } = fakeClient()
    const mounted = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([resource]) }))
    vi.mocked(client.resourcePlan).mockResolvedValue(resourcePlan(resource.id))
    vi.mocked(client.resourceAction).mockReturnValue(
      new Promise<OperationRef>((resolve) => { resolveAction = resolve }),
    )
    await tick()
    await mounted.wrapper.get('[role="option"]').trigger('click')
    await tick()
    await mounted.wrapper.get('button[aria-label="Install Resource"]').trigger('click')
    await tick()
    const confirm = document.body.querySelector('button[aria-label="Confirm install"]') as HTMLButtonElement
    confirm.click()
    await tick()
    expect(document.body.querySelector('.resource-actions__spinner')).not.toBeNull()
    resolveAction({ id: 91, kind: 'mutation' })
    await tick()
    expect(document.body.querySelector('.resource-actions__spinner')).toBeNull()
  })
})
