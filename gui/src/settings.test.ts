// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import type { mount } from '@vue/test-utils'
import {
  RequestError,
  type Configuration,
  type OperationRef,
} from './api/client'
import {
  configuration,
  event,
  fakeClient,
  mountWorkspace,
  tick,
} from './test-support'

const settingsConfiguration = (overrides: Partial<Configuration> = {}): Configuration =>
  configuration({
    userTargets: ['devin', 'future-harness'],
    organizationTargets: ['claude'],
    supportedTargets: ['claude', 'devin', 'windsurf'],
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

// spec: gui-configuration/save-user-harness-targets-without-losing-unknown-identifiers
describe('Harness settings dialog', () => {
  it('lists supported and root-unknown targets with a read-only organization section', async () => {
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
    const future = checkboxByLabel('future-harness')
    expect(isChecked(devin)).toBe(true)
    expect(isChecked(claude)).toBe(false)
    expect(isChecked(future)).toBe(true)
    expect(dialog!.textContent).toContain('Unsupported by this version')
    expect(dialog!.textContent).toContain('claude')
    expect(dialog!.textContent).toContain('managed outside the GUI')
    const orgList = dialog!.querySelector('[aria-label="Organization targets"]')
    expect(orgList).not.toBeNull()
    expect(orgList!.querySelectorAll('[role="checkbox"], input[type="checkbox"]')).toHaveLength(0)
    expect(orgList!.textContent).toContain('claude')
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

    const cancel = Array.from(document.body.querySelectorAll<HTMLElement>('[role="dialog"] button')).find(
      (b) => b.textContent === 'Cancel',
    )!
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
      targets: ['claude', 'devin', 'future-harness'],
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
    checkboxByLabel('future-harness').click()
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

  it('closes the dialog after an accepted save even when settlement arrived first', async () => {
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
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    const settingsButton = wrapper.find('button[aria-label="Settings"]')
    expect(document.activeElement).toBe(settingsButton.element)
  })

  it('keeps fields disabled until the opening metadata load resolves', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const cancel = Array.from(document.body.querySelectorAll<HTMLElement>('[role="dialog"] button')).find(
      (b) => b.textContent === 'Cancel',
    )!
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
    const fieldset = dialog.querySelector<HTMLFieldSetElement>('fieldset')!
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

    const cancel = Array.from(document.body.querySelectorAll<HTMLElement>('[role="dialog"] button')).find(
      (b) => b.textContent === 'Cancel',
    )!
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
    expect(isChecked(checkboxByLabel('future-harness'))).toBe(true)
  })

  it('disables the target fieldset when the root is not editable', async () => {
    const { client, push } = settingsClient({ editable: false })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const fieldset = document.body.querySelector<HTMLFieldSetElement>('[role="dialog"] fieldset')
    expect(fieldset).not.toBeNull()
    expect(fieldset!.disabled).toBe(true)
  })

  it('offers no checkbox for unknown identifiers absent from the root', async () => {
    const { client, push } = settingsClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSettings(wrapper)
    const offered = Array.from(
      document.body.querySelectorAll('[role="dialog"] [role="checkbox"], [role="dialog"] input[type="checkbox"]'),
    ).map((el) => el.getAttribute('aria-label'))
    expect(offered).not.toContain('Target brand-new-unknown')
    expect(offered).toEqual(
      expect.arrayContaining(['Target claude', 'Target devin', 'Target windsurf', 'Target future-harness']),
    )
  })
})
