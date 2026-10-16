// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import {
  RequestError,
  type Configuration,
  type OperationRef,
  type SourceInput,
} from './api/client'
import {
  configuration,
  event,
  fakeClient,
  mountWorkspace,
  snapshot,
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

function sourceKindRadio(kind: SourceInput['kind']): HTMLInputElement {
  const group = dialog().querySelector<HTMLFieldSetElement>('fieldset[aria-label="Source kind"]')
  expect(group).not.toBeNull()
  const radio = group!.querySelector<HTMLInputElement>(`input[type="radio"][name="source-kind"][value="${kind}"]`)
  expect(radio).not.toBeNull()
  return radio!
}

function sourceField(label: string): HTMLInputElement {
  const input = dialog().querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)
  expect(input).not.toBeNull()
  return input!
}

async function setSourceField(label: string, value: string) {
  const input = sourceField(label)
  input.value = value
  input.dispatchEvent(new Event('input'))
  await tick()
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
    expect(dialogButton('Next').disabled).toBe(true)

    dialogButton('Back').click()
    await tick()
    expect(isChecked(checkbox('devin'))).toBe(true)
    expect(client.configure).not.toHaveBeenCalled()

    dialogButton('Next').click()
    await tick()
    consentRadio(/no/i).click()
    await tick()
    expect(dialogButton('Next').disabled).toBe(false)
    expect(client.configure).not.toHaveBeenCalled()
    dialogButton('Next').click()
    await tick()
    expect(sourceKindRadio('directory').checked).toBe(true)
    expect(sourceField('Source location').disabled).toBe(false)
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
    dialogButton('Next').click()
    await tick()
    expect(sourceKindRadio('directory').checked).toBe(true)
    expect(client.configure).not.toHaveBeenCalled()
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
    dialogButton('Next').click()
    await tick()
    expect(sourceKindRadio('directory').checked).toBe(true)
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
    dialogButton('Next').click()
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

  it('disables source controls and Finish when canInitialize flips false on the sources step', async () => {
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
    dialogButton('Next').click()
    await tick()
    expect(dialogButton('Finish setup').disabled).toBe(false)
    expect(sourceField('Source location').disabled).toBe(false)

    ;(client.configuration as ReturnType<typeof vi.fn>).mockResolvedValue(
      firstRunConfiguration({ canInitialize: false }),
    )
    await controller.loadConfiguration()
    await tick()

    expect(dialog().textContent).toContain('Initialization is not currently available.')
    expect(dialogButton('Finish setup').disabled).toBe(true)
    expect(sourceField('Source location').disabled).toBe(true)
    dialogButton('Finish setup').click()
    await tick()
    expect(client.configure).not.toHaveBeenCalled()

    dialogButton('Back').click()
    await tick()
    expect(consentRadio(/yes/i).checked).toBe(true)
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

    dialogButton('Next').click()
    await tick()
    expect(sourceKindRadio('directory').checked).toBe(true)
    expect(client.configure).not.toHaveBeenCalled()
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

    dialogButton('Next').click()
    await tick()
    expect(sourceKindRadio('directory').checked).toBe(true)
    expect(client.configure).not.toHaveBeenCalled()
    dialogButton('Finish setup').click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({ action: 'initialize', targets: ['devin'] })
    expect(controller.state.operation).toBeNull()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect((client.configuration as ReturnType<typeof vi.fn>).mock.calls.length).toBeGreaterThanOrEqual(2)
  })

  // spec: gui-first-run/optional-source-step-uses-the-configuration-form
  it('queues typed sources locally and submits them only when Finish setup is pressed', async () => {
    const { client, push } = firstRunClient({ consentRequired: false, editable: false })
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    dialogButton('Next').click()
    await tick()
    expect(sourceKindRadio('directory').checked).toBe(true)
    expect(sourceField('Source location').disabled).toBe(false)

    const directory = '/queued/directory'
    await setSourceField('Source location', directory)
    dialogButton('Add source').click()
    await tick()
    expect(dialog().querySelector('[aria-label="Pending sources"]')?.textContent).toContain(directory)
    expect(sourceField('Source location').value).toBe('')
    expect(client.configure).not.toHaveBeenCalled()

    const gitLocation = 'https://example.test/repo.git'
    sourceKindRadio('git').click()
    await tick()
    await setSourceField('Source location', gitLocation)
    await setSourceField('Source ref', 'main')
    await setSourceField('Source subpath', 'skills')
    dialogButton('Add source').click()
    await tick()
    expect(dialog().querySelector('[aria-label="Pending sources"]')?.textContent).toContain('Ref main')
    expect(dialog().querySelector('[aria-label="Pending sources"]')?.textContent).toContain('Subpath skills')
    expect(client.configure).not.toHaveBeenCalled()

    const endpoint = 'https://marketplace.example/services/catalog'
    sourceKindRadio('marketplace').click()
    await tick()
    await setSourceField('Source location', endpoint)
    dialogButton('Add source').click()
    await tick()
    expect(dialog().querySelector('[aria-label="Pending sources"]')?.textContent).toContain(endpoint)
    expect(sourceField('Source location').value).toBe('')
    expect(client.configure).not.toHaveBeenCalled()

    dialogButton(`Remove source ${gitLocation}`).click()
    await tick()
    const pending = dialog().querySelector('[aria-label="Pending sources"]')!
    expect(pending.textContent).toContain(directory)
    expect(pending.textContent).toContain(endpoint)
    expect(pending.textContent).not.toContain(gitLocation)
    expect(client.configure).not.toHaveBeenCalled()

    const operation: OperationRef = { id: 9, kind: 'mutation' }
    ;(client.configure as ReturnType<typeof vi.fn>).mockResolvedValue(operation)
    dialogButton('Finish setup').click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({
      action: 'initialize',
      targets: ['devin'],
      sources: [
        { kind: 'directory', location: directory },
        { kind: 'marketplace', location: endpoint },
      ],
    })
  })

  const organizationSourceKinds = [
    { kind: 'directory', source: { id: 'org-dir', kind: 'directory', location: '/org/directory', ref: '', path: '', sourceHarness: '' } },
    { kind: 'git', source: { id: 'org-git', kind: 'git', location: 'https://org.example/repo.git', ref: 'main', path: '', sourceHarness: '' } },
    { kind: 'marketplace', source: { id: 'org-marketplace', kind: 'marketplace', location: 'https://org.example/services/catalog', ref: '', path: '', sourceHarness: '' } },
  ] as const

  // spec: gui-first-run/organization-sources-skip-the-source-step
  it.each(organizationSourceKinds)('skips the optional source step when organization metadata projects a %s source', async ({ source }) => {
    const { client, push } = firstRunClient({ consentRequired: false, organizationSources: [source] })
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    expect(dialog().querySelector('fieldset[aria-label="Source kind"]')).toBeNull()
    expect(dialog().querySelector('[aria-label="Pending sources"]')).toBeNull()
    expect(dialog().querySelector('button[aria-label="Next"]')).toBeNull()
    expect(dialogButton('Finish setup').disabled).toBe(false)
    dialogButton('Finish setup').click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({ action: 'initialize', targets: ['devin'] })
  })

  // spec: gui-first-run/cancel-first-run-before-completion
  it.each(['targets', 'consent', 'sources'] as const)('cancels at %s and reopens with fresh drafts', async (cancelAt) => {
    const { client, push } = firstRunClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    if (cancelAt === 'targets') {
      checkbox('claude').click()
      await tick()
    } else {
      dialogButton('Next').click()
      await tick()
      if (cancelAt === 'consent') {
        consentRadio(/yes/i).click()
        await tick()
      } else {
        consentRadio(/yes/i).click()
        await tick()
        dialogButton('Next').click()
        await tick()
        await setSourceField('Source location', '/cancelled/directory')
        dialogButton('Add source').click()
        await tick()
        expect(dialog().querySelector('[aria-label="Pending sources"]')?.textContent).toContain('/cancelled/directory')
      }
    }

    dialogButton('Cancel').click()
    await tick()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(client.configure).not.toHaveBeenCalled()
    expect(client.action).not.toHaveBeenCalled()

    await wrapper.find('button[aria-label="Set up"]').trigger('click')
    await tick()
    expect(isChecked(checkbox('devin'))).toBe(true)
    expect(isChecked(checkbox('claude'))).toBe(false)
    if (cancelAt !== 'targets') {
      dialogButton('Next').click()
      await tick()
      expect(consentRadio(/yes/i).checked).toBe(false)
      expect(consentRadio(/no/i).checked).toBe(false)
      if (cancelAt === 'sources') {
        consentRadio(/no/i).click()
        await tick()
        dialogButton('Next').click()
        await tick()
        expect(dialog().querySelector('[aria-label="Pending sources"]')).toBeNull()
        expect(sourceKindRadio('directory').checked).toBe(true)
        expect(sourceField('Source location').value).toBe('')
      }
    }
    expect(client.configure).not.toHaveBeenCalled()
  })

  // spec: gui-first-run/optional-source-step-uses-the-configuration-form
  it('keeps the queued source drafts after initialize preflight rejection', async () => {
    const { client, push } = firstRunClient({ consentRequired: false })
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    ;(client.configure as ReturnType<typeof vi.fn>).mockRejectedValue(
      new RequestError(400, 'localapi.invalid_request'),
    )

    dialogButton('Next').click()
    await tick()
    await setSourceField('Source location', '/queued/correct-after-preflight')
    dialogButton('Add source').click()
    await tick()
    expect(client.configure).not.toHaveBeenCalled()
    dialogButton('Finish setup').click()
    await tick()

    expect(client.configure).toHaveBeenCalledWith({
      action: 'initialize',
      targets: ['devin'],
      sources: [{ kind: 'directory', location: '/queued/correct-after-preflight' }],
    })
    expect(dialog().textContent).toContain('The action could not be started')
    expect(dialog().querySelector('[aria-label="Pending sources"]')?.textContent).toContain('/queued/correct-after-preflight')
    expect(client.action).not.toHaveBeenCalled()
  })

  // spec: gui-first-run/optional-source-failure-keeps-initialized-configuration
  it('keeps a failed optional source out of registered settings and leaves Sources retry reachable', async () => {
    const { client, push } = firstRunClient({ consentRequired: false })
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    const directory = { id: 'first-directory-id', kind: 'directory' as const, location: '/first-directory', ref: '', path: '', sourceHarness: '' }
    const afterInit = firstRunConfiguration({
      firstRun: false,
      canInitialize: false,
      rootPresent: true,
      editable: true,
      userTargets: ['devin'],
      userSources: [directory],
    })
    ;(client.configuration as ReturnType<typeof vi.fn>).mockResolvedValue(afterInit)
    const initializeOperation: OperationRef = { id: 12, kind: 'mutation' }
    const retryOperation: OperationRef = { id: 13, kind: 'mutation' }
    ;(client.configure as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce(initializeOperation)
      .mockResolvedValueOnce(retryOperation)

    dialogButton('Next').click()
    await tick()
    await setSourceField('Source location', directory.location)
    dialogButton('Add source').click()
    await tick()
    sourceKindRadio('marketplace').click()
    await tick()
    const failedEndpoint = 'http://127.0.0.1:43210/services/catalog'
    await setSourceField('Source location', failedEndpoint)
    dialogButton('Add source').click()
    await tick()
    expect(client.configure).not.toHaveBeenCalled()
    dialogButton('Finish setup').click()
    await tick()
    expect(client.configure).toHaveBeenNthCalledWith(1, {
      action: 'initialize',
      targets: ['devin'],
      sources: [
        { kind: 'directory', location: directory.location },
        { kind: 'marketplace', location: failedEndpoint },
      ],
    })

    push(event({ sequence: 2, generation: 2, operation: initializeOperation }))
    push(event({
      sequence: 3,
      generation: 2,
      operation: null,
      payload: snapshot([], {
        diagnostics: [{
          severity: 'error',
          category: 'mutation',
          code: 'localapi.optional-source-failed',
          message: 'add optional source: Marketplace registration failed',
        }],
        settledOperation: initializeOperation,
      }),
    }))
    push(event({
      sequence: 4,
      generation: 2,
      operation: initializeOperation,
      type: 'notification',
      payload: { severity: 'error', message: 'mutation failed' },
    }))
    await tick()

    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(controller.state.diagnostics.some((d) => d.code === 'localapi.optional-source-failed')).toBe(true)
    expect(controller.state.notifications.some((n) => n.message === 'mutation failed')).toBe(true)
    expect(controller.state.notifications.some((n) => n.message === 'mutation complete')).toBe(false)
    const sourcesButton = wrapper.find('button[aria-label="Sources"]')
    expect(sourcesButton.exists()).toBe(true)
    await sourcesButton.trigger('click')
    await tick()
    const settings = dialog()
    const registered = settings.querySelector('[aria-label="User sources"]')
    expect(registered?.textContent).toContain(directory.location)
    expect(registered?.textContent).not.toContain(failedEndpoint)
    expect(settings.textContent).toContain('add optional source: Marketplace registration failed')

    sourceKindRadio('marketplace').click()
    await tick()
    await setSourceField('Source location', failedEndpoint)
    dialogButton('Add source').click()
    await tick()
    expect(client.configure).toHaveBeenNthCalledWith(2, {
      action: 'add-source',
      source: { kind: 'marketplace', location: failedEndpoint },
    })
  })

  // spec: gui-first-run/optional-source-step-uses-the-configuration-form
  it('backs from Sources to consent and preserves queued drafts', async () => {
    const { client, push } = firstRunClient()
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    dialogButton('Next').click()
    await tick()
    consentRadio(/no/i).click()
    await tick()
    dialogButton('Next').click()
    await tick()
    await setSourceField('Source location', '/pending/retained')
    dialogButton('Add source').click()
    await tick()
    expect(dialog().querySelector('[aria-label="Pending sources"]')?.textContent).toContain('/pending/retained')

    dialogButton('Back').click()
    await tick()
    expect(consentRadio(/no/i).checked).toBe(true)
    expect(dialog().querySelector('fieldset[aria-label="Source kind"]')).toBeNull()
    dialogButton('Back').click()
    await tick()
    expect(isChecked(checkbox('devin'))).toBe(true)
    dialogButton('Next').click()
    await tick()
    expect(consentRadio(/no/i).checked).toBe(true)
    dialogButton('Next').click()
    await tick()
    expect(dialog().querySelector('[aria-label="Pending sources"]')?.textContent).toContain('/pending/retained')
    expect(client.configure).not.toHaveBeenCalled()
  })

  // spec: gui-first-run/optional-source-step-uses-the-configuration-form
  it('backs from Sources to targets when consent is skipped', async () => {
    const { client, push } = firstRunClient({ consentRequired: false })
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    dialogButton('Next').click()
    await tick()
    expect(sourceKindRadio('directory').checked).toBe(true)
    dialogButton('Back').click()
    await tick()
    expect(isChecked(checkbox('devin'))).toBe(true)
    expect(dialog().querySelector('input[aria-label="Yes, enable basic telemetry"]')).toBeNull()
    expect(client.configure).not.toHaveBeenCalled()
  })

  // spec: gui-first-run/organization-sources-skip-the-source-step
  it.each(organizationSourceKinds)('finishes after consent without Sources when organization declares a %s source', async ({ source }) => {
    const { client, push } = firstRunClient({ organizationSources: [source] })
    mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    dialogButton('Next').click()
    await tick()
    consentRadio(/no/i).click()
    await tick()
    expect(dialog().querySelector('fieldset[aria-label="Source kind"]')).toBeNull()
    expect(dialog().querySelector('button[aria-label="Next"]')).toBeNull()
    expect(dialogButton('Finish setup').disabled).toBe(false)
    dialogButton('Finish setup').click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({
      action: 'initialize',
      targets: ['devin'],
      consent: false,
    })
  })
})
