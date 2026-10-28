// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import type { mount } from '@vue/test-utils'
import {
  RequestError,
  type Configuration,
  type OperationRef,
} from './api/client'
import { source } from './fixtures'
import {
  configuration,
  event,
  fakeClient,
  mountWorkspace,
  snapshot,
  row,
  tick,
} from './test-support'

const settingsConfiguration = (overrides: Partial<Configuration> = {}): Configuration =>
  configuration({
    userTargets: ['devin', 'codex'],
    organizationTargets: ['claude'],
    supportedTargets: ['claude', 'codex', 'devin', 'windsurf'],
    managedOutsideGUI: ['included files', 'per-source configuration', 'other settings'],
    ...overrides,
  })

const settingsClient = (overrides: Partial<Configuration> = {}) =>
  fakeClient({ configuration: settingsConfiguration(overrides) })

async function openSettings(wrapper: ReturnType<typeof mount>) {
  const button = wrapper.find('button[aria-label="Settings"]')
  expect(button.exists()).toBe(true)
  await button.trigger('click')
  await tick()
}

function checkboxByLabel(label: string): HTMLElement {
  const input = document.body.querySelector<HTMLElement>(
    `[role="dialog"] [role="checkbox"][aria-label="Target ${label}"], [role="dialog"] input[aria-label="Target ${label}"]`,
  )
  if (input != null) {
    return input
  }
  throw new Error(`checkbox ${label} not found in ${document.body.innerHTML}`)
}

function isChecked(element: HTMLElement): boolean {
  return (
    element.getAttribute('aria-checked') === 'true' ||
    element.getAttribute('data-state') === 'checked' ||
    (element as HTMLInputElement).checked === true
  )
}

describe('Harness settings dialog', () => {
  // spec: gui-configuration/organization-harness-targets-are-read-only
  it('lists supported targets with a read-only organization section', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    expect(client.configuration).toHaveBeenCalledTimes(2)

    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog).not.toBeNull()
    const devin = checkboxByLabel('devin')
    const claude = checkboxByLabel('claude')
    const codex = checkboxByLabel('codex')
    expect(isChecked(devin)).toBe(true)
    expect(isChecked(claude)).toBe(false)
    expect(isChecked(codex)).toBe(true)
    expect(dialog!.textContent).toContain('claude')
    expect(dialog!.textContent).toContain('managed outside the GUI')
    const orgList = dialog!.querySelector('[aria-label="Organization targets"]')
    expect(orgList).not.toBeNull()
    expect(orgList!.querySelectorAll('[role="checkbox"], input[type="checkbox"]')).toHaveLength(0)
    expect(orgList!.textContent).toContain('claude')
  })

  // spec: gui-configuration/organization-sources-are-read-only
  it('opens Settings on General and keeps sources under the Sources tab', async () => {
    const { client, push } = settingsClient({
      userSources: [
        { id: 'user-dir', kind: 'directory', location: '/var/regs', ref: '', path: '', sourceHarness: '', rowSourceId: '' },
      ],
      organizationSources: [
        { id: 'org-dir', kind: 'directory', location: '/opt/org-regs', ref: '', path: '', sourceHarness: '', rowSourceId: '' },
      ],
    })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog.querySelector('.gui-modal__frame')?.getAttribute('aria-label')).toBe('Settings')
    expect(dialog.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toContain('General')
    expect(dialog.querySelector('button[aria-label="Manage sources"]')).toBeNull()

    dialog.querySelector<HTMLElement>('#settings-tab-sources')!.click()
    await tick()
    const sources = dialog.querySelector('#settings-panel-sources')!
    expect(sources.querySelector('[aria-label="Organization sources"]')?.textContent).toContain('/opt/org-regs')
    expect(sources.querySelector('[aria-label="User sources"]')?.textContent).toContain('/var/regs')
    expect(client.configure).not.toHaveBeenCalled()
    expect(client.action).not.toHaveBeenCalled()
  })

  // spec: gui-configuration/organization-sources-are-read-only
  it('renders hostile source declarations as inert text', async () => {
    const hostile = '<img src=x onerror=alert(1)>'
    const { client, push } = settingsClient({
      organizationSources: [
        { id: 'org-hostile', kind: 'directory', location: hostile, ref: '', path: '', sourceHarness: '<script>alert(1)</script>', rowSourceId: '' },
      ],
    })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await wrapper.find('button[aria-label="Sources"]').trigger('click')
    await tick()
    const dialog = document.body.querySelector('[role="dialog"]')!
    const orgSection = dialog.querySelector('[aria-label="Organization sources"]')!
    expect(orgSection.querySelector('img')).toBeNull()
    expect(orgSection.querySelector('script')).toBeNull()
    expect(orgSection.textContent).toContain(hostile)
    expect(client.configure).not.toHaveBeenCalled()
  })

  // spec: session-interactions/toggling-harness-targets-does-not-write
  it('toggles a draft without any RPC until Save and discards it on cancel', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const devin = checkboxByLabel('devin')
    devin.click()
    await tick()
    expect(client.configure).not.toHaveBeenCalled()
    expect(client.action).not.toHaveBeenCalled()

    const cancel = document.body.querySelector<HTMLElement>('button[aria-label="Close dialog"]')!
    cancel.click()
    await tick()

    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(client.configure).not.toHaveBeenCalled()
    const settingsButton = wrapper.find('button[aria-label="Settings"]')
    expect(document.activeElement).toBe(settingsButton.element)

    await openSettings(wrapper)
    expect(isChecked(checkboxByLabel('devin'))).toBe(true)
  })

  // spec: session-interactions/save-selected-harnesses
  it('saves the draft selection through the guarded operation and reloads metadata', async () => {
    const { client, push } = settingsClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    checkboxByLabel('claude').click()
    await tick()

    const save = Array.from(document.body.querySelectorAll<HTMLElement>('[role="dialog"] button')).find(
      (b) => b.textContent === 'Save targets',
    )!
    save.click()
    await tick()

    expect(client.configure).toHaveBeenCalledTimes(1)
    expect(client.configure).toHaveBeenCalledWith({
      action: 'save-targets',
      targets: ['claude', 'codex', 'devin'],
    })
    expect(controller.state.operation).toEqual({ id: 8, kind: 'targets' })

    const operation = { id: 8, kind: 'targets' } as OperationRef
    push(event({ sequence: 2, generation: 2, operation }))
    push(event({ sequence: 3, generation: 2, operation: null }))
    push(
      event({
        sequence: 4,
        generation: 2,
        operation,
        type: 'notification',
        payload: { severity: 'info', message: 'targets complete' },
      }),
    )
    await tick()
    expect(controller.state.operation).toBeNull()
    expect((client.configuration as ReturnType<typeof vi.fn>).mock.calls.length).toBeGreaterThanOrEqual(2)
  })

  // spec: session-interactions/empty-selection
  it('keeps Save disabled for an empty draft, an uneditable root, or a busy session', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const save = () =>
      Array.from(document.body.querySelectorAll('[role="dialog"] button')).find(
        (b) => b.textContent === 'Save targets',
      ) as HTMLButtonElement
    expect(save().disabled).toBe(false)
    checkboxByLabel('devin').click()
    checkboxByLabel('codex').click()
    await tick()
    expect(save().disabled).toBe(true)
  })

  it('keeps Save disabled when the root is not editable', async () => {
    const { client, push } = settingsClient()
    ;(client.configuration as ReturnType<typeof vi.fn>).mockResolvedValue(
      settingsConfiguration({ editable: false, rootPresent: false }),
    )
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const save = Array.from(document.body.querySelectorAll('[role="dialog"] button')).find(
      (b) => b.textContent === 'Save targets',
    ) as HTMLButtonElement
    expect(save.disabled).toBe(true)
    expect(document.body.querySelector('[role="dialog"]')!.textContent).toContain('not editable')
  })

  it('protects configuration metadata with a latest-request sequence', async () => {
    const { client, push } = settingsClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    const resolvers: ((value: Configuration) => void)[] = []
    ;(client.configuration as ReturnType<typeof vi.fn>).mockImplementation(
      () => new Promise<Configuration>((resolve) => resolvers.push(resolve)),
    )
    await openSettings(wrapper)
    void controller.loadConfiguration()
    resolvers[1](settingsConfiguration({ userTargets: ['windsurf'] }))
    await tick()
    resolvers[0](settingsConfiguration({ userTargets: ['claude'] }))
    await tick()
    expect(controller.state.configuration?.userTargets).toEqual(['windsurf'])
    expect(isChecked(checkboxByLabel('windsurf'))).toBe(true)
  })

  it('does not reset the unsaved draft on a window focus refresh', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    checkboxByLabel('devin').click()
    await tick()
    window.dispatchEvent(new Event('focus'))
    await tick()
    expect(client.snapshot).toHaveBeenCalled()
    expect(isChecked(checkboxByLabel('devin'))).toBe(false)
  })

  it('keeps the dialog open and reports the failure when the save is rejected', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    ;(client.configure as ReturnType<typeof vi.fn>).mockRejectedValue(new RequestError(400, 'localapi.invalid_request'))

    await openSettings(wrapper)
    const save = Array.from(document.body.querySelectorAll<HTMLElement>('[role="dialog"] button')).find(
      (b) => b.textContent === 'Save targets',
    )!
    save.click()
    await tick()
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog).not.toBeNull()
    expect(dialog!.textContent).toContain('The action could not be started')
  })

  it('keeps the window open after an accepted save even when settlement arrived first', async () => {
    const { client, push } = settingsClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    const operation: OperationRef = { id: 8, kind: 'targets' }
    ;(client.configure as ReturnType<typeof vi.fn>).mockImplementation(async () => {
      push(event({ sequence: 2, generation: 2, operation }))
      push(event({ sequence: 3, generation: 2, operation: null }))
      push(
        event({
          sequence: 4,
          generation: 2,
          operation,
          type: 'notification',
          payload: { severity: 'info', message: 'targets complete' },
        }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
      return operation
    })

    await openSettings(wrapper)
    const save = Array.from(document.body.querySelectorAll<HTMLElement>('[role="dialog"] button')).find(
      (b) => b.textContent === 'Save targets',
    )!
    save.click()
    await tick()

    expect(client.configure).toHaveBeenCalledTimes(1)
    expect(controller.state.operation).toBeNull()
    expect(controller.state.phase).toBeNull()
    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull()
  })

  it('keeps fields disabled until the opening metadata load resolves', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const cancel = document.body.querySelector<HTMLElement>('button[aria-label="Close dialog"]')!
    cancel.click()
    await tick()

    let resolveLoad: (value: Configuration) => void = () => undefined
    ;(client.configuration as ReturnType<typeof vi.fn>).mockImplementation(
      () => new Promise<Configuration>((resolve) => (resolveLoad = resolve)),
    )
    const button = wrapper.find('button[aria-label="Settings"]')
    await button.trigger('click')
    await tick()

    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog).not.toBeNull()
    const fieldset = dialog.querySelector<HTMLFieldSetElement>('#settings-panel-targets fieldset')!
    const save = Array.from(dialog.querySelectorAll<HTMLElement>('button')).find(
      (b) => b.textContent === 'Save targets',
    ) as HTMLButtonElement
    expect(fieldset.disabled).toBe(true)
    expect(save.disabled).toBe(true)

    resolveLoad(settingsConfiguration())
    await tick()
    expect(fieldset.disabled).toBe(false)
    expect(save.disabled).toBe(false)
    expect(isChecked(checkboxByLabel('devin'))).toBe(true)
  })

  it('ignores a cancelled opening load when the dialog is reopened', async () => {
    const { client, push } = settingsClient()
    const resolvers: ((value: Configuration) => void)[] = []
    ;(client.configuration as ReturnType<typeof vi.fn>).mockImplementation(
      () => new Promise<Configuration>((resolve) => resolvers.push(resolve)),
    )
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    resolvers[0](settingsConfiguration())
    await tick()

    const button = wrapper.find('button[aria-label="Settings"]')
    await button.trigger('click')
    await tick()

    const cancel = document.body.querySelector<HTMLElement>('button[aria-label="Close dialog"]')!
    cancel.click()
    await tick()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()

    resolvers[1](settingsConfiguration({ userTargets: ['windsurf'] }))
    await tick()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()

    await button.trigger('click')
    await tick()
    resolvers[2](settingsConfiguration())
    await tick()
    expect(isChecked(checkboxByLabel('devin'))).toBe(true)
    expect(isChecked(checkboxByLabel('codex'))).toBe(true)
  })

  it('disables the target fieldset when the root is not editable', async () => {
    const { client, push } = settingsClient({ editable: false })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const fieldset = document.body.querySelector<HTMLFieldSetElement>('#settings-panel-targets fieldset')
    expect(fieldset).not.toBeNull()
    expect(fieldset!.disabled).toBe(true)
  })

  it('offers checkboxes only for supported targets', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const offered = Array.from(
      document.body.querySelectorAll('[role="dialog"] [role="checkbox"], [role="dialog"] input[type="checkbox"]'),
    ).map((el) => el.getAttribute('aria-label'))
    expect(offered.filter((label) => label?.startsWith('Target '))).toEqual(['Target claude', 'Target codex', 'Target devin', 'Target windsurf'])
  })

  it('shows Unsaved changes while the draft differs and toggles once from the row name', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const dialog = document.body.querySelector('[role="dialog"]')!
    const unsaved = () => dialog.querySelector('.settings__dirty')
    expect(unsaved()).toBeNull()

    const devinName = Array.from(dialog.querySelectorAll<HTMLElement>('.settings__target')).find((el) => el.textContent === 'devin')!
    devinName.click()
    await tick()
    expect(isChecked(checkboxByLabel('devin'))).toBe(false)
    expect(unsaved()?.textContent).toContain('Unsaved changes')

    devinName.click()
    await tick()
    expect(isChecked(checkboxByLabel('devin'))).toBe(true)
    expect(unsaved()).toBeNull()
  })

  it('marks a user target that the organization also sets', async () => {
    const { client, push } = settingsClient({ userTargets: ['claude', 'devin'], organizationTargets: ['claude'] })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const dialog = document.body.querySelector('[role="dialog"]')!
    const claudeRow = checkboxByLabel('claude').closest('label')!
    expect(claudeRow.textContent).toContain('Also set by organization')
    const devinRow = checkboxByLabel('devin').closest('label')!
    expect(devinRow.textContent).not.toContain('Also set by organization')
    expect(dialog.querySelector('[aria-label="Organization targets"] input[type="checkbox"]')).toBeNull()
  })

})

describe('tabbed Settings window', () => {
  it('opens on General from the gear and on Sources from the sidebar Sources control', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toContain('General')
    expect(dialog.querySelector('#settings-tab-targets')?.getAttribute('aria-selected')).toBe('false')
    expect(dialog.querySelector('#settings-tab-general')?.getAttribute('aria-controls')).toBe('settings-panel-general')
    dialog.querySelector<HTMLElement>('button[aria-label="Close dialog"]')!.click()
    await tick()

    await wrapper.get('button[aria-label="Sources"]').trigger('click')
    await tick()
    expect(document.querySelector('#settings-tab-sources')?.getAttribute('aria-selected')).toBe('true')
  })

  it('moves between tabs with the arrow and Home/End keys', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    await openSettings(wrapper)
    const dialog = document.body.querySelector('[role="dialog"]')!
    const tab = (id: string) => dialog.querySelector<HTMLElement>(`#settings-tab-${id}`)!
    const selected = () => dialog.querySelector('[role="tab"][aria-selected="true"]')?.id

    tab('general').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    await tick()
    expect(selected()).toBe('settings-tab-targets')
    tab('targets').dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true }))
    await tick()
    expect(selected()).toBe('settings-tab-sources')
    tab('sources').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    await tick()
    expect(selected()).toBe('settings-tab-general')
    tab('general').dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true }))
    await tick()
    expect(selected()).toBe('settings-tab-general')
  })

  it('keeps the target draft across tab switches, reverts it, and keeps the window open after Save', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    await openSettings(wrapper)
    const dialog = document.body.querySelector('[role="dialog"]')!
    dialog.querySelector<HTMLElement>('#settings-tab-targets')!.click()
    await tick()
    checkboxByLabel('devin').click()
    await tick()
    expect(dialog.textContent).toContain('Unsaved changes')

    dialog.querySelector<HTMLElement>('#settings-tab-general')!.click()
    await tick()
    dialog.querySelector<HTMLElement>('#settings-tab-targets')!.click()
    await tick()
    expect(isChecked(checkboxByLabel('devin'))).toBe(false)

    const revert = Array.from(dialog.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent?.trim() === 'Revert')!
    revert.click()
    await tick()
    expect(isChecked(checkboxByLabel('devin'))).toBe(true)
    expect(dialog.textContent).not.toContain('Unsaved changes')

    checkboxByLabel('devin').click()
    await tick()
    const save = Array.from(dialog.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent?.trim() === 'Save targets')!
    save.click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({ action: 'save-targets', targets: expect.any(Array) })
    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull()
  })
})

describe('accepted target save', () => {
  it('keeps the submitted selection until the write settles and then reports it clean', async () => {
    const { client, push } = settingsClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    await openSettings(wrapper)
    const dialog = document.body.querySelector('[role="dialog"]')!
    dialog.querySelector<HTMLElement>('#settings-tab-targets')!.click()
    await tick()
    checkboxByLabel('claude').click()
    await tick()

    const operation: OperationRef = { id: 12, kind: 'targets' }
    ;(client.configure as ReturnType<typeof vi.fn>).mockImplementation(async () => {
      push(event({ sequence: 2, generation: 2, operation }))
      return operation
    })
    const save = Array.from(dialog.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent?.trim() === 'Save targets')!
    save.click()
    await tick()

    expect(client.configure).toHaveBeenCalledTimes(1)
    expect(controller.state.operation).toEqual(operation)
    expect(isChecked(checkboxByLabel('claude'))).toBe(true)
    expect(isChecked(checkboxByLabel('devin'))).toBe(true)

    ;(client.configuration as ReturnType<typeof vi.fn>).mockResolvedValue(
      settingsConfiguration({ userTargets: ['devin', 'codex', 'claude'] }),
    )
    push(event({ sequence: 3, generation: 2, operation: null, payload: snapshot([row('solo')], { settledOperation: operation }) }))
    await tick()

    expect(isChecked(checkboxByLabel('claude'))).toBe(true)
    expect(dialog.textContent).not.toContain('Unsaved changes')
  })
})
