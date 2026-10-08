// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
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

const firstRunConfiguration = (overrides: Partial<Configuration> = {}): Configuration =>
  configuration({
    userTargets: [],
    supportedTargets: ['claude', 'devin', 'windsurf'],
    rootPresent: false,
    editable: false,
    firstRun: true,
    canInitialize: true,
    detectedTargets: ['devin'],
    consentRequired: true,
    telemetryDestination: 'telemetry.example.com',
    retentionPolicyURL: 'https://example.com/retention',
    ...overrides,
  })

const firstRunClient = (overrides: Partial<Configuration> = {}) =>
  fakeClient({ configuration: firstRunConfiguration(overrides) })

function dialog(): HTMLElement {
  const found = document.body.querySelector<HTMLElement>('[role="dialog"]')
  expect(found).not.toBeNull()
  return found!
}

function dialogButton(label: string): HTMLButtonElement {
  const button = Array.from(dialog().querySelectorAll<HTMLElement>('button')).find(
    (b) => b.textContent === label || b.getAttribute('aria-label') === label,
  )
  expect(button, `button ${label} in ${dialog().textContent}`).toBeTruthy()
  return button as HTMLButtonElement
}

function checkbox(id: string): HTMLElement {
  const input = dialog().querySelector<HTMLElement>(
    `[role="checkbox"][aria-label="Target ${id}"], input[aria-label="Target ${id}"]`,
  )
  expect(input, `checkbox ${id}`).not.toBeNull()
  return input!
}

function isChecked(element: HTMLElement): boolean {
  return (
    element.getAttribute('aria-checked') === 'true' ||
    element.getAttribute('data-state') === 'checked' ||
    (element as HTMLInputElement).checked === true
  )
}

function consentRadio(name: RegExp): HTMLInputElement {
  const all = Array.from(dialog().querySelectorAll<HTMLInputElement>('input[type="radio"]'))
  const found = all.find((r) => name.test(r.getAttribute('aria-label') ?? ''))
  expect(found).toBeTruthy()
  return found!
}

// spec: gui-first-run/absent-user-personal-root-starts-first-run
describe('First-run setup', () => {
  it('opens automatically only for an absent root, never for an existing or invalid one', async () => {
    const { client, push } = firstRunClient()
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    expect(client.configuration).toHaveBeenCalledTimes(1)
    expect(dialog().textContent).toContain('Set up Agent Marketplace')
    expect(isChecked(checkbox('devin'))).toBe(true)
    expect(isChecked(checkbox('claude'))).toBe(false)
    expect(isChecked(checkbox('windsurf'))).toBe(false)
    expect(client.configure).not.toHaveBeenCalled()
  })

  it('does not open when the root exists, and keeps ordinary settings intact', async () => {
    const { client, push } = firstRunClient({
      firstRun: false,
      canInitialize: false,
      rootPresent: true,
      editable: true,
      userTargets: ['devin'],
    })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    const settings = wrapper.find('button[aria-label="Settings"]')
    expect(settings.exists()).toBe(true)
  })

  it('does not open when metadata reports an invalid existing root', async () => {
    const { client, push } = firstRunClient({
      firstRun: false,
      canInitialize: false,
      rootPresent: true,
      editable: false,
    })
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
  })

  // spec: gui-first-run/detected-harness-targets-are-preselected-and-editable
  it('preselects detected targets, allows changes, and requires a non-empty selection', async () => {
    const { client, push } = firstRunClient()
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    const next = () => dialogButton('Next')
    expect(next().disabled).toBe(false)

    checkbox('devin').click()
    await tick()
    expect(next().disabled).toBe(true)

    checkbox('windsurf').click()
    await tick()
    expect(next().disabled).toBe(false)
    expect(client.configure).not.toHaveBeenCalled()
  })

  // spec: gui-first-run/unmanaged-first-run-presents-init-equivalent-consent
  it('shows consent without a default answer and requires an explicit choice', async () => {
    const { client, push } = firstRunClient()
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    dialogButton('Next').click()
    await tick()
    expect(dialog().textContent).toContain('telemetry.example.com')
    expect(dialog().textContent).toContain('https://example.com/retention')
    expect(dialog().textContent).toContain('basic telemetry')

    expect(consentRadio(/yes/i).checked).toBe(false)
    expect(consentRadio(/no/i).checked).toBe(false)
    expect(dialogButton('Finish setup').disabled).toBe(true)

    dialogButton('Back').click()
    await tick()
    expect(isChecked(checkbox('devin'))).toBe(true)
    expect(client.configure).not.toHaveBeenCalled()

    dialogButton('Next').click()
    await tick()
    consentRadio(/no/i).click()
    await tick()
    expect(dialogButton('Finish setup').disabled).toBe(false)
    dialogButton('Finish setup').click()
    await tick()

    expect(client.configure).toHaveBeenCalledWith({
      action: 'initialize',
      targets: ['devin'],
      consent: false,
    })
  })

  it('sends an affirmative consent boolean', async () => {
    const { client, push } = firstRunClient()
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    dialogButton('Next').click()
    await tick()
    consentRadio(/yes/i).click()
    await tick()
    dialogButton('Finish setup').click()
    await tick()

    expect(client.configure).toHaveBeenCalledWith({
      action: 'initialize',
      targets: ['devin'],
      consent: true,
    })
  })

  // spec: gui-first-run/organization-telemetry-level-skips-consent
  it('omits consent entirely when the organization manages telemetry', async () => {
    const { client, push } = firstRunClient({ consentRequired: false })
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    expect(dialog().querySelectorAll('input[type="radio"]')).toHaveLength(0)
    dialogButton('Finish setup').click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({
      action: 'initialize',
      targets: ['devin'],
    })
  })

  // spec: gui-first-run/cancel-first-run-before-completion
  it('drops the draft on cancel and escape without any request, and reopens only explicitly', async () => {
    const { client, push } = firstRunClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    expect(dialog()).toBeTruthy()

    checkbox('claude').click()
    await tick()
    dialogButton('Cancel').click()
    await tick()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(client.configure).not.toHaveBeenCalled()
    expect(client.action).not.toHaveBeenCalled()

    window.dispatchEvent(new Event('focus'))
    await tick()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()

    const setup = wrapper.find('button[aria-label="Set up"]')
    expect(setup.exists()).toBe(true)
    expect(document.activeElement).toBe(setup.element)
    await setup.trigger('click')
    await tick()
    expect(dialog()).toBeTruthy()
    expect(isChecked(checkbox('devin'))).toBe(true)
    expect(isChecked(checkbox('claude'))).toBe(false)

    dialog().dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await tick()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(client.configure).not.toHaveBeenCalled()
  })

  it('keeps the modal open with the draft when the request is rejected', async () => {
    const { client, push } = firstRunClient()
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    ;(client.configure as ReturnType<typeof vi.fn>).mockRejectedValue(
      new RequestError(400, 'localapi.invalid_request'),
    )

    checkbox('windsurf').click()
    await tick()
    dialogButton('Next').click()
    await tick()
    consentRadio(/yes/i).click()
    await tick()
    dialogButton('Finish setup').click()
    await tick()

    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull()
    expect(dialog().textContent).toContain('The action could not be started')
  })

  // spec: gui-first-run/absent-user-personal-root-starts-first-run
  it('shows an eligibility notice and blocks submission when canInitialize is false', async () => {
    const { client, push } = firstRunClient({ canInitialize: false })
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    expect(dialog().textContent).toContain('Initialization is not currently available.')
    expect(dialogButton('Next').disabled).toBe(true)
    dialogButton('Next').click()
    await tick()
    expect(client.configure).not.toHaveBeenCalled()

    dialogButton('Cancel').click()
    await tick()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
  })

  it('disables Finish and preserves the draft when canInitialize flips false on the consent step', async () => {
    const { client, push } = firstRunClient()
    const { controller } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    checkbox('windsurf').click()
    await tick()
    dialogButton('Next').click()
    await tick()
    consentRadio(/yes/i).click()
    await tick()
    expect(dialogButton('Finish setup').disabled).toBe(false)

    ;(client.configuration as ReturnType<typeof vi.fn>).mockResolvedValue(
      firstRunConfiguration({ canInitialize: false }),
    )
    await controller.loadConfiguration()
    await tick()

    expect(dialog().textContent).toContain('Initialization is not currently available.')
    expect(dialogButton('Finish setup').disabled).toBe(true)
    expect(consentRadio(/yes/i).checked).toBe(true)
    dialogButton('Finish setup').click()
    await tick()
    expect(client.configure).not.toHaveBeenCalled()

    dialogButton('Back').click()
    await tick()
    expect(isChecked(checkbox('windsurf'))).toBe(true)
  })

  // spec: gui-first-run/detected-harness-targets-are-preselected-and-editable
  it('warns statically when detection is unavailable and still allows manual selection', async () => {
    const { client, push } = firstRunClient({ detectedTargets: [], detectionUnavailable: true })
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    expect(dialog().textContent).toContain('Harness detection is unavailable; select targets manually.')
    expect(isChecked(checkbox('devin'))).toBe(false)
    checkbox('devin').click()
    await tick()
    expect(dialogButton('Next').disabled).toBe(false)
    expect(client.configure).not.toHaveBeenCalled()
  })

  it('closes after an accepted submission even when settlement already arrived', async () => {
    const { client, push } = firstRunClient({ consentRequired: false })
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    const operation: OperationRef = { id: 8, kind: 'mutation' }
    ;(client.configure as ReturnType<typeof vi.fn>).mockImplementation(async () => {
      push(event({ sequence: 2, generation: 2, operation }))
      push(event({ sequence: 3, generation: 2, operation: null }))
      push(
        event({
          sequence: 4,
          generation: 2,
          operation,
          type: 'notification',
          payload: { severity: 'info', message: 'mutation complete' },
        }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
      return operation
    })

    dialogButton('Finish setup').click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({ action: 'initialize', targets: ['devin'] })
    expect(controller.state.operation).toBeNull()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect((client.configuration as ReturnType<typeof vi.fn>).mock.calls.length).toBeGreaterThanOrEqual(2)

    ;(client.configuration as ReturnType<typeof vi.fn>).mockResolvedValue(
      firstRunConfiguration({ firstRun: false, canInitialize: false, rootPresent: true, editable: true, userTargets: ['devin'] }),
    )
    push(event({ sequence: 5, generation: 3 }))
    await tick()
    const focused = document.activeElement as HTMLElement | null
    const names = ['Set up', 'Settings', 'Refresh']
    const labelled = focused?.getAttribute('aria-label') ?? ''
    expect(names).toContain(labelled)
    expect(wrapper.find('button[aria-label="Settings"]').exists()).toBe(true)
  })

  it('settles a submission whose events all arrive before the accepted response, with no prior snapshot', async () => {
    const { client, push } = firstRunClient({ consentRequired: false })
    const { controller } = mountWorkspace(client)
    await tick()

    const operation: OperationRef = { id: 8, kind: 'mutation' }
    ;(client.configure as ReturnType<typeof vi.fn>).mockImplementation(async () => {
      push(event({ sequence: 1, generation: 1, operation }))
      push(event({ sequence: 2, generation: 1, operation: null }))
      push(
        event({
          sequence: 3,
          generation: 1,
          operation,
          type: 'notification',
          payload: { severity: 'info', message: 'mutation complete' },
        }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
      return operation
    })

    dialogButton('Finish setup').click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({ action: 'initialize', targets: ['devin'] })
    expect(controller.state.operation).toBeNull()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect((client.configuration as ReturnType<typeof vi.fn>).mock.calls.length).toBeGreaterThanOrEqual(2)
  })
})
