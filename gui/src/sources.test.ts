// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import type { mount } from '@vue/test-utils'
import { RequestError, type Configuration, type OperationRef, type SourceInput } from './api/client'
import { configuration, event, fakeClient, mountWorkspace, snapshot, tick } from './test-support'

const dirSource = {
  id: 'user-dir-id',
  name: '',
  kind: 'directory' as const,
  location: '/var/regs',
  ref: '',
  path: '',
  sourceHarness: '',
  rowSourceId: '',
}
const gitSource = {
  id: 'user-git-id',
  name: '',
  kind: 'git' as const,
  location: 'https://example.com/repo.git',
  ref: 'main',
  path: '',
  sourceHarness: '',
  rowSourceId: '',
}
const marketplaceSource = {
  id: 'user-mp-id',
  name: '',
  kind: 'marketplace' as const,
  location: 'https://marketplace.example',
  ref: '',
  path: '',
  sourceHarness: '',
  rowSourceId: '',
}

const sourcesConfiguration = (overrides: Partial<Configuration> = {}): Configuration =>
  configuration({ userSources: [dirSource, gitSource, marketplaceSource], ...overrides })

const sourcesClient = (overrides: Partial<Configuration> = {}) =>
  fakeClient({ configuration: sourcesConfiguration(overrides) })

const groupedSources = [
  { ...gitSource, id: 'git-alpha', path: 'plugins/alpha' },
  { ...gitSource, id: 'git-beta', path: 'plugins/beta', ref: 'next' },
  { ...dirSource, id: 'solo-directory', location: '/var/solo' },
]

async function mountSources(overrides: Partial<Configuration> = { userSources: groupedSources }) {
  const { client, push } = sourcesClient(overrides)
  const { wrapper } = mountWorkspace(client)
  push(event({ sequence: 1 }))
  await tick()
  return { client, wrapper }
}

async function openSources(wrapper: ReturnType<typeof mount>) {
  const button = wrapper.find('button[aria-label="Sources"]')
  expect(button.exists()).toBe(true)
  await button.trigger('click')
  await tick()
}

function dialog(): HTMLElement {
  const el = document.body.querySelector<HTMLElement>('[role="dialog"]')
  if (el == null) {
    throw new Error('settings dialog not open')
  }
  return el
}

function field(label: string): HTMLInputElement {
  const el = dialog().querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)
  if (el == null) {
    throw new Error(`field ${label} not found`)
  }
  return el
}

function button(name: string): HTMLElement {
  const el = Array.from(dialog().querySelectorAll<HTMLElement>('button')).find(
    (b) => b.getAttribute('aria-label') === name || b.textContent === name,
  )
  if (el == null) {
    throw new Error(`button ${name} not found`)
  }
  return el
}

async function setInput(label: string, value: string) {
  const input = field(label)
  input.value = value
  input.dispatchEvent(new Event('input'))
  await tick()
}

function sourceKindRadio(value: SourceInput['kind'], label: string): HTMLInputElement {
  const group = dialog().querySelector<HTMLFieldSetElement>('fieldset[aria-label="Source kind"]')
  if (group == null) {
    throw new Error('source kind group not found')
  }
  expect(group.querySelector('legend')?.textContent?.trim()).toBe('Source kind')
  const radio = group.querySelector<HTMLInputElement>(`input[type="radio"][name="source-kind"][value="${value}"]`)
  if (radio == null) {
    throw new Error(`source kind radio ${value} not found`)
  }
  expect(radio.closest('label')?.textContent?.trim()).toBe(label)
  return radio
}

async function chooseSourceKind(value: SourceInput['kind'], label: string) {
  sourceKindRadio(value, label).click()
  await tick()
}

async function settleOperation(controller: { state: { operation: OperationRef | null } }, push: (e: ReturnType<typeof event>) => void, operation: OperationRef, seq: number) {
  push(event({ sequence: seq, generation: 2, operation }))
  push(event({ sequence: seq + 1, generation: 2, operation: null }))
  push(
    event({
      sequence: seq + 2,
      generation: 2,
      operation,
      type: 'notification',
      payload: { severity: 'info', message: 'mutation complete' },
    }),
  )
  await tick()
  expect(controller.state.operation).toBeNull()
}

function mutationError(code: string, message: string) {
  return { severity: 'error' as const, category: 'mutation' as const, code, message }
}

function failAdd(push: (e: ReturnType<typeof event>) => void, operation: OperationRef, seq: number, diagnostics: ReturnType<typeof mutationError>[]) {
  push(event({ sequence: seq, generation: 2, operation }))
  push(event({ sequence: seq + 1, generation: 2, operation: null, payload: snapshot([], { diagnostics }) }))
  push(
    event({
      sequence: seq + 2,
      generation: 2,
      operation,
      type: 'notification',
      payload: { severity: 'error', message: 'mutation failed' },
    }),
  )
}

describe('Source settings', () => {
  it('styles Settings Add source as secondary with the plus icon', async () => {
    const { client, push } = sourcesClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    const addButton = button('Add source')
    expect(addButton.classList.contains('gui-button--secondary')).toBe(true)
    const icon = addButton.querySelector('svg.gui-icon')
    expect(icon).not.toBeNull()
    expect(Array.from(icon!.querySelectorAll('path'), (path) => path.getAttribute('d'))).toEqual([
      'M8 3v10',
      'M3 8h10',
    ])
  })

  it('removes a user source through its opaque id and reloads metadata', async () => {
    const { client, push } = sourcesClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    const remove = button(`Remove source ${dirSource.location}`)
    remove.click()
    await tick()
    expect(client.configure).not.toHaveBeenCalled()

    ;(client.configure as ReturnType<typeof vi.fn>).mockResolvedValue({ id: 9, kind: 'mutation' } as OperationRef)
    button('Confirm remove').click()
    await tick()

    expect(client.configure).toHaveBeenCalledTimes(1)
    expect(client.configure).toHaveBeenCalledWith({ action: 'remove-source', id: 'user-dir-id' })
    expect(controller.state.operation).toEqual({ id: 9, kind: 'mutation' })

    const calls = (client.configuration as ReturnType<typeof vi.fn>).mock.calls.length
    await settleOperation(controller, push, { id: 9, kind: 'mutation' }, 2)
    expect((client.configuration as ReturnType<typeof vi.fn>).mock.calls.length).toBeGreaterThan(calls)
  })

  it('cancelling the removal confirmation sends no request', async () => {
    const { client, push } = sourcesClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    button(`Remove source ${dirSource.location}`).click()
    await tick()
    button('Cancel remove').click()
    await tick()

    expect(client.configure).not.toHaveBeenCalled()
    expect(dialog().querySelector('[aria-label="Confirm removal"]')).toBeNull()
  })

  it('cancels Marketplace registration removal without sending a request', async () => {
    const { client, push } = sourcesClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    button(`Remove source ${marketplaceSource.location}`).click()
    await tick()
    expect(client.configure).not.toHaveBeenCalled()

    button('Cancel remove').click()
    await tick()

    expect(client.configure).not.toHaveBeenCalled()
    expect(dialog().querySelector('[aria-label="Confirm removal"]')).toBeNull()
    expect(dialog().querySelector('[aria-label="User sources"]')?.textContent).toContain(marketplaceSource.location)
  })

  it('preserves a Marketplace draft and registration when removal is refused', async () => {
    const { client, push } = sourcesClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    const operation: OperationRef = { id: 9, kind: 'mutation' }
    ;(client.configure as ReturnType<typeof vi.fn>).mockResolvedValue(operation)

    await openSources(wrapper)
    await chooseSourceKind('marketplace', 'Marketplace endpoint')
    const draft = 'https://marketplace.example/draft'
    await setInput('Source location', draft)
    button(`Remove source ${marketplaceSource.location}`).click()
    await tick()
    button('Confirm remove').click()
    await tick()

    expect(client.configure).toHaveBeenCalledWith({ action: 'remove-source', id: marketplaceSource.id })
    push(event({ sequence: 2, generation: 2, operation }))
    push(
      event({
        sequence: 3,
        generation: 2,
        operation: null,
        payload: snapshot([], {
          diagnostics: [
            {
              severity: 'error',
              category: 'mutation',
              code: 'config.edit.source-in-use',
              message: 'config.edit.source-in-use: Marketplace source has resources',
            },
          ],
        }),
      }),
    )
    push(
      event({
        sequence: 4,
        generation: 2,
        operation,
        type: 'notification',
        payload: { severity: 'error', message: 'mutation failed' },
      }),
    )
    await tick()

    expect(controller.state.operation).toBeNull()
    expect(dialog().textContent).toContain('config.edit.source-in-use')
    expect(sourceKindRadio('marketplace', 'Marketplace endpoint').checked).toBe(true)
    expect(field('Source location').value).toBe(draft)
    expect(dialog().querySelector('[aria-label="User sources"]')?.textContent).toContain(marketplaceSource.location)
    expect((button(`Remove source ${marketplaceSource.location}`) as HTMLButtonElement).disabled).toBe(false)
  })

  it('updates the Marketplace source list in place after removal settles', async () => {
    const { client, push } = sourcesClient()
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    const operation: OperationRef = { id: 9, kind: 'mutation' }
    ;(client.configure as ReturnType<typeof vi.fn>).mockResolvedValue(operation)
    ;(client.configuration as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce(sourcesConfiguration())
      .mockResolvedValue(sourcesConfiguration({ userSources: [dirSource, gitSource] }))

    await openSources(wrapper)
    button(`Remove source ${marketplaceSource.location}`).click()
    await tick()
    button('Confirm remove').click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({ action: 'remove-source', id: marketplaceSource.id })

    await settleOperation(controller, push, operation, 2)
    await tick()

    const list = dialog().querySelector('[aria-label="User sources"]')!
    expect(list.textContent).not.toContain(marketplaceSource.location)
    expect(dialog()).toBeDefined()
    expect(controller.state.operation).toBeNull()
    expect(controller.state.requesting).toBe(false)
    expect(field('Source location').disabled).toBe(false)
    expect((button(`Remove source ${dirSource.location}`) as HTMLButtonElement).disabled).toBe(false)
    expect((button('Add source') as HTMLButtonElement).disabled).toBe(true)
    expect(() => button(`Remove source ${marketplaceSource.location}`)).toThrow()
  })

  it.each([
    {
      source: { id: 'org-dir', name: '', kind: 'directory' as const, location: '/opt/org-regs', ref: '', path: '', sourceHarness: '', rowSourceId: '' },
      texts: ['DIRECTORY', 'ORG', '/opt/org-regs'],
      absent: ['Ref ', 'Subpath '],
    },
    {
      source: {
        id: 'org-git',
        name: '',
        kind: 'git' as const,
        location: 'https://example.com/org.git',
        ref: 'main',
        path: 'nested',
        sourceHarness: 'devin',
        rowSourceId: '',
      },
      texts: ['GIT', 'https://example.com/org.git', 'main · nested · layout devin'],
      absent: [],
    },
    {
      source: { id: 'org-mp', name: '', kind: 'marketplace' as const, location: 'https://org-mp.example', ref: '', path: '', sourceHarness: '', rowSourceId: '' },
      texts: ['MARKETPLACE', 'ORG', 'https://org-mp.example'],
      absent: ['Ref ', 'Subpath '],
    },
  ])('keeps the organization $source.kind source free of any controls', async ({ source, texts, absent }) => {
    const { client, push } = sourcesClient({ organizationSources: [source] })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    const org = dialog().querySelector('[aria-label="Organization sources"]')!
    expect(org.textContent).toContain('Managed by your organization; read-only.')
    for (const text of texts) {
      expect(org.textContent).toContain(text)
    }
    for (const text of absent) {
      expect(org.textContent).not.toContain(text)
    }
    expect(org.querySelectorAll('button, input, select, a')).toHaveLength(0)
    expect(client.configure).not.toHaveBeenCalled()
  })

  it('shows managed-outside labels while a user source is being edited', async () => {
    const { client, push } = sourcesClient({
      managedOutsideGUI: ['included files', 'per-source configuration', 'other settings'],
    })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    const section = dialog().querySelector('[aria-label="Managed outside the GUI"]')!
    expect(section.textContent).toContain('Included files, per-source configuration, and other settings are managed outside the GUI.')

    button(`Remove source ${dirSource.location}`).click()
    await tick()
    expect(section.textContent).toContain('Included files, per-source configuration, and other settings are managed outside the GUI.')
    button('Cancel remove').click()
    await tick()
    expect(client.configure).not.toHaveBeenCalled()
  })

  // spec: gui-configuration/add-source-request-error-renders-in-the-section
  // spec: gui-configuration/add-source-error-clears-after-a-successful-add
  it('keeps a failed add source request beside its form and clears it after a successful add', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const failure = new RequestError(502, 'localapi.source_unavailable')
    vi.mocked(client.configure)
      .mockRejectedValueOnce(failure)
      .mockResolvedValueOnce({ id: 9, kind: 'mutation' } as OperationRef)
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    await setInput('Source location', '/opt/new-regs')
    button('Add source').click()
    await tick()

    expect(client.configure).toHaveBeenCalledWith({
      action: 'add-source',
      source: { kind: 'directory', location: '/opt/new-regs' },
    })
    const addSection = dialog().querySelector<HTMLElement>('[aria-label="Add user source"]')!
    expect(addSection.querySelector('[role="alert"]')?.textContent).toContain('The action could not be started')
    expect(dialog().querySelector('.settings-window__content > [role="alert"]')).toBeNull()

    button('Add source').click()
    await tick()
    expect(client.configure).toHaveBeenCalledTimes(2)
    expect(addSection.querySelector('[role="alert"]')).toBeNull()
  })

  it('keeps a pre-existing mutation error at the top when an add source request is accepted', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const saveFailure = {
      severity: 'error' as const,
      category: 'mutation' as const,
      code: 'targets.failed',
      message: 'Saving targets failed',
    }
    const addFailure = {
      severity: 'error' as const,
      category: 'mutation' as const,
      code: 'source.unavailable',
      message: 'Source unavailable',
    }
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([], { diagnostics: [saveFailure] }) }))
    await tick()
    ;(client.configure as ReturnType<typeof vi.fn>).mockResolvedValue({ id: 9, kind: 'mutation' } as OperationRef)

    await openSources(wrapper)
    await setInput('Source location', '/opt/new-regs')
    button('Add source').click()
    await tick()
    push(event({ sequence: 2, generation: 2, operation: { id: 9, kind: 'mutation' } }))
    push(
      event({
        sequence: 3,
        generation: 2,
        operation: null,
        payload: snapshot([], { diagnostics: [saveFailure, addFailure] }),
      }),
    )
    await tick()

    const top = dialog().querySelector<HTMLElement>('.settings-window__content')!
    const addSection = dialog().querySelector<HTMLElement>('[aria-label="Add user source"]')!
    expect(Array.from(top.querySelectorAll(':scope > [role="alert"]')).map((el) => el.textContent)).toEqual([
      'Saving targets failed',
    ])
    expect(addSection.textContent).not.toContain('Saving targets failed')
    expect(addSection.querySelector('[role="alert"]')?.textContent).toContain('Source unavailable')
  })

  it('shows a busy rejection of an add source request beside its form', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    vi.mocked(client.configure).mockRejectedValueOnce(new RequestError(409, 'localapi.busy'))
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    await setInput('Source location', '/opt/new-regs')
    button('Add source').click()
    await tick()

    const addSection = dialog().querySelector<HTMLElement>('[aria-label="Add user source"]')!
    expect(addSection.querySelector('[role="alert"]')?.textContent).toContain('The action could not be started')
    expect(dialog().querySelector('.settings-window__content > [role="alert"]')).toBeNull()
  })

  it('keeps a failed add source error in the Add source section after Settings is reopened', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    const operation: OperationRef = { id: 9, kind: 'mutation' }
    vi.mocked(client.configure).mockResolvedValue(operation)

    await openSources(wrapper)
    await setInput('Source location', '/opt/new-regs')
    button('Add source').click()
    await tick()
    failAdd(push, operation, 2, [mutationError('config.edit.include-owned', 'Source is declared in an included fragment')])
    await tick()

    button('Close dialog').click()
    await tick()
    await openSources(wrapper)

    expect(dialog().querySelector('[aria-label="Add user source"]')?.textContent).toContain('Source is declared in an included fragment')
    expect(dialog().querySelector('.settings-window__content > [role="alert"]')).toBeNull()
  })

  it('keeps a retried add source error in the Add source section while the retry is pending and after it fails again', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    const first: OperationRef = { id: 9, kind: 'mutation' }
    const retry: OperationRef = { id: 10, kind: 'mutation' }
    vi.mocked(client.configure).mockResolvedValueOnce(first).mockResolvedValueOnce(retry)
    const diagnostic = mutationError('config.edit.include-owned', 'Source is declared in an included fragment')

    await openSources(wrapper)
    await setInput('Source location', '/opt/new-regs')
    button('Add source').click()
    await tick()
    failAdd(push, first, 2, [diagnostic])
    await tick()

    button('Add source').click()
    await tick()
    const addSection = dialog().querySelector<HTMLElement>('[aria-label="Add user source"]')!
    expect(addSection.textContent).toContain(diagnostic.message)
    expect(dialog().querySelector('.settings-window__content > [role="alert"]')).toBeNull()

    failAdd(push, retry, 5, [diagnostic])
    await tick()
    expect(addSection.textContent).toContain(diagnostic.message)
    expect(dialog().querySelector('.settings-window__content > [role="alert"]')).toBeNull()
  })

  it('adds a git source with an explicit ref and optional subpath', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    await chooseSourceKind('git', 'Git registry')
    await setInput('Source location', 'https://example.com/reg.git')
    await setInput('Source ref', 'main')
    await setInput('Source subpath', 'sub/dir')
    button('Add source').click()
    await tick()

    expect(client.configure).toHaveBeenCalledWith({
      action: 'add-source',
      source: { kind: 'git', location: 'https://example.com/reg.git', ref: 'main', path: 'sub/dir' },
    })
  })

  it('keeps Add disabled until required fields are filled or the root is uneditable', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    expect((button('Add source') as HTMLButtonElement).disabled).toBe(true)
    await setInput('Source location', '/x')
    expect((button('Add source') as HTMLButtonElement).disabled).toBe(false)

    await chooseSourceKind('git', 'Git registry')
    expect((button('Add source') as HTMLButtonElement).disabled).toBe(true)
    await setInput('Source ref', 'main')
    expect((button('Add source') as HTMLButtonElement).disabled).toBe(false)
    expect(client.configure).not.toHaveBeenCalled()
  })

  it('disables the form and removal when the root is not editable', async () => {
    const { client, push } = sourcesClient({ editable: false })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    const addSection = dialog().querySelector<HTMLElement>('[aria-label="Add user source"]')
    expect(addSection).not.toBeNull()
    expect((addSection!.querySelector('fieldset') as HTMLFieldSetElement).disabled).toBe(true)
    expect((button('Add source') as HTMLButtonElement).disabled).toBe(true)
    expect((button(`Remove source ${dirSource.location}`) as HTMLButtonElement).disabled).toBe(true)
    expect((button(`Remove source ${marketplaceSource.location}`) as HTMLButtonElement).disabled).toBe(true)
  })

  it('cancelling the dialog discards the draft and sends no request', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    await setInput('Source location', '/draft')
    button('Close dialog').click()
    await tick()

    expect(dialog).toThrow()
    expect(client.configure).not.toHaveBeenCalled()
  })

  it('reopening the dialog resets the draft and pending removal to defaults', async () => {
    const { client, push } = sourcesClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    await chooseSourceKind('git', 'Git registry')
    await setInput('Source location', 'https://example.com/reg.git')
    await setInput('Source ref', 'main')
    button(`Remove source ${dirSource.location}`).click()
    await tick()
    expect(dialog().querySelector('[aria-label="Confirm removal"]')).not.toBeNull()

    button('Close dialog').click()
    await tick()
    await openSources(wrapper)

    expect(sourceKindRadio('directory', 'Directory registry').checked).toBe(true)
    expect(field('Source location').value).toBe('')
    expect(dialog().querySelector('input[aria-label="Source ref"]')).toBeNull()
    expect(dialog().querySelector('[aria-label="Confirm removal"]')).toBeNull()
    expect(client.configure).not.toHaveBeenCalled()
  })

  it('shows the authoritative mutation failure and keeps the submitted draft', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    ;(client.configure as ReturnType<typeof vi.fn>).mockResolvedValue({ id: 9, kind: 'mutation' } as OperationRef)

    await openSources(wrapper)
    await setInput('Source location', '/opt/new-regs')
    button('Add source').click()
    await tick()
    expect(controller.state.operation).toEqual({ id: 9, kind: 'mutation' })

    push(event({ sequence: 2, generation: 2, operation: { id: 9, kind: 'mutation' } }))
    push(
      event({
        sequence: 3,
        generation: 2,
        operation: null,
        payload: snapshot([], {
          diagnostics: [
            {
              severity: 'error',
              category: 'mutation',
              code: 'config.edit.include-owned',
              message: 'config.edit.include-owned: source "x" is declared in an included fragment',
            },
          ],
        }),
      }),
    )
    push(
      event({
        sequence: 4,
        generation: 2,
        operation: { id: 9, kind: 'mutation' },
        type: 'notification',
        payload: { severity: 'error', message: 'mutation failed' },
      }),
    )
    await tick()

    expect(controller.state.operation).toBeNull()
    const alerts = Array.from(dialog().querySelectorAll('[role="alert"]')).map((el) => el.textContent)
    expect(alerts).toContain('config.edit.include-owned: source "x" is declared in an included fragment')
    expect(field('Source location').value).toBe('/opt/new-regs')
    expect(dialog().textContent).not.toContain('complete')
  })

  it('updates the source list after a successful operation and returns to idle', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    ;(client.configure as ReturnType<typeof vi.fn>).mockResolvedValue({ id: 9, kind: 'mutation' } as OperationRef)
    ;(client.configuration as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce(sourcesConfiguration({ userSources: [] }))
      .mockResolvedValue(sourcesConfiguration({ userSources: [dirSource] }))

    await openSources(wrapper)
    await setInput('Source location', dirSource.location)
    button('Add source').click()
    await tick()
    await settleOperation(controller, push, { id: 9, kind: 'mutation' }, 2)
    await tick()

    const list = dialog().querySelector('[aria-label="User sources"]')!
    expect(list.textContent).toContain(dirSource.location)
    expect(controller.state.operation).toBeNull()
    expect(controller.state.requesting).toBe(false)
  })

  it('does not resurrect a busy state when the accepted response lands after settlement', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    const operation: OperationRef = { id: 9, kind: 'mutation' }
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

    await openSources(wrapper)
    await setInput('Source location', '/opt/new-regs')
    button('Add source').click()
    await tick()

    expect(client.configure).toHaveBeenCalledTimes(1)
    expect(controller.state.operation).toBeNull()
    expect(controller.state.phase).toBeNull()
  })

  it('reports a rejected mutation without leaving the form', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    ;(client.configure as ReturnType<typeof vi.fn>).mockRejectedValue(
      new RequestError(400, 'localapi.invalid_request'),
    )

    await openSources(wrapper)
    await setInput('Source location', '/x')
    button('Add source').click()
    await tick()

    expect(dialog().textContent).toContain('The action could not be started')
    expect(field('Source location').value).toBe('/x')
    button('Add source').click()
    await tick()
    expect(client.configure).toHaveBeenCalledTimes(2)
  })

  it('submits a Marketplace endpoint with only kind and location', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    await chooseSourceKind('marketplace', 'Marketplace endpoint')
    const endpoint = 'https://marketplace.example/services/catalog'
    await setInput('Source location', endpoint)
    button('Add source').click()
    await tick()

    expect(client.configure).toHaveBeenCalledWith({
      action: 'add-source',
      source: { kind: 'marketplace', location: endpoint },
    })
    expect(dialog().querySelector('input[aria-label="Source ref"]')).toBeNull()
    expect(dialog().querySelector('input[aria-label="Source subpath"]')).toBeNull()
  })

  it('keeps a Marketplace draft after preflight rejection', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    ;(client.configure as ReturnType<typeof vi.fn>).mockRejectedValue(
      new RequestError(400, 'localapi.invalid_request'),
    )

    await openSources(wrapper)
    await chooseSourceKind('marketplace', 'Marketplace endpoint')
    const endpoint = 'https://marketplace.example/services/catalog'
    await setInput('Source location', endpoint)
    button('Add source').click()
    await tick()

    expect(client.configure).toHaveBeenCalledWith({
      action: 'add-source',
      source: { kind: 'marketplace', location: endpoint },
    })
    expect(dialog().textContent).toContain('The action could not be started')
    expect(sourceKindRadio('marketplace', 'Marketplace endpoint').checked).toBe(true)
    expect(field('Source location').value).toBe(endpoint)
    expect(dialog().querySelector('.gui-modal__frame')?.getAttribute('aria-label')).toBe('Settings')
    expect(dialog().querySelector('#settings-panel-sources')).not.toBeNull()
  })

  it('keeps a Marketplace draft after an accepted operation fails', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    const operation: OperationRef = { id: 9, kind: 'mutation' }
    ;(client.configure as ReturnType<typeof vi.fn>).mockResolvedValue(operation)

    await openSources(wrapper)
    await chooseSourceKind('marketplace', 'Marketplace endpoint')
    const endpoint = 'https://marketplace.example/services/catalog'
    await setInput('Source location', endpoint)
    button('Add source').click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({
      action: 'add-source',
      source: { kind: 'marketplace', location: endpoint },
    })

    push(event({ sequence: 2, generation: 2, operation }))
    push(
      event({
        sequence: 3,
        generation: 2,
        operation: null,
        payload: snapshot([], {
          diagnostics: [
            {
              severity: 'error',
              category: 'mutation',
              code: 'marketplace.registration-failed',
              message: 'Marketplace registration failed',
            },
          ],
        }),
      }),
    )
    push(
      event({
        sequence: 4,
        generation: 2,
        operation,
        type: 'notification',
        payload: { severity: 'error', message: 'mutation failed' },
      }),
    )
    await tick()

    expect(controller.state.operation).toBeNull()
    expect(dialog().textContent).toContain('Marketplace registration failed')
    expect(sourceKindRadio('marketplace', 'Marketplace endpoint').checked).toBe(true)
    expect(field('Source location').value).toBe(endpoint)
    expect((button('Add source') as HTMLButtonElement).disabled).toBe(false)
  })

  it('updates the user source list in place after Marketplace registration settles', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { controller, wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()
    const operation: OperationRef = { id: 9, kind: 'mutation' }
    ;(client.configure as ReturnType<typeof vi.fn>).mockResolvedValue(operation)
    ;(client.configuration as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce(sourcesConfiguration({ userSources: [] }))
      .mockResolvedValue(sourcesConfiguration({ userSources: [marketplaceSource] }))

    await openSources(wrapper)
    await chooseSourceKind('marketplace', 'Marketplace endpoint')
    await setInput('Source location', marketplaceSource.location)
    const addButton = button('Add source')
    addButton.click()
    await tick()
    await settleOperation(controller, push, operation, 2)
    await tick()

    const list = dialog().querySelector('[aria-label="User sources"]')!
    expect(list.textContent).toContain(marketplaceSource.location)
    expect(list.textContent).toContain('MARKETPLACE')
    expect(dialog()).toBeDefined()
    expect(controller.state.operation).toBeNull()
    expect(controller.state.requesting).toBe(false)
    expect((addButton as HTMLButtonElement).disabled).toBe(false)
  })

  // spec: gui-shell/sources-sharing-a-location-are-grouped
  // spec: gui-shell/group-folds-by-pointer-and-keyboard
  it('groups shared locations in the sidebar and Settings but leaves a single source unwrapped', async () => {
    const { wrapper } = await mountSources()
    const sidebar = wrapper.find('[aria-label="Sources list"]')
    const sidebarGroup = sidebar.find('button[aria-expanded]')
    for (const text of ['repo', 'plugins/alpha', 'plugins/beta', 'next']) expect(sidebar.text()).toContain(text)
    await sidebarGroup.trigger('click')
    expect(sidebarGroup.attributes('aria-expanded')).toBe('false')
    await sidebarGroup.trigger('keydown', { key: 'Enter' })
    expect(sidebarGroup.attributes('aria-expanded')).toBe('true')
    await sidebarGroup.trigger('keydown', { key: ' ' })
    expect(sidebarGroup.attributes('aria-expanded')).toBe('false')
    await openSources(wrapper)
    const sources = dialog().querySelector('#settings-panel-sources')!
    const settingsGroup = sources.querySelector<HTMLElement>('button[aria-expanded]')!
    settingsGroup.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await tick()
    settingsGroup.click()
    await tick()
    expect(settingsGroup.getAttribute('aria-expanded')).toBe('true')
    for (const text of ['plugins/alpha', 'plugins/beta', 'next']) expect(sources.textContent).toContain(text)
    expect(sources.querySelector('[data-source-id="solo-directory"]')?.textContent).toContain('/var/solo')
  })

  // spec: gui-shell/selecting-a-sidebar-source-reveals-it-in-settings
  // spec: gui-shell/revealing-a-source-unfolds-its-folded-group
  // spec: gui-shell/sidebar-add-control-focuses-the-add-source-form
  it('reveals a selected source and focuses the add form from the sidebar', async () => {
    const { wrapper } = await mountSources()
    await openSources(wrapper)
    await setInput('Search sources', 'plugins/beta')
    expect(Array.from(dialog().querySelectorAll('.source-card')).map((card) => card.getAttribute('data-source-id'))).toEqual(['git-beta'])
    const group = dialog().querySelector<HTMLElement>('#settings-panel-sources button[aria-expanded]')!
    group.click()
    await tick()
    expect(group.getAttribute('aria-expanded')).toBe('false')
    await wrapper.find('button[data-source-id="git-alpha"]').trigger('click')
    await tick()

    const search = dialog().querySelector<HTMLInputElement>('input[aria-label="Search sources"]')
    const card = dialog().querySelector<HTMLElement>('#settings-panel-sources [data-source-id="git-alpha"]')
    expect(group.getAttribute('aria-expanded')).toBe('true')
    expect(search?.value).toBe('')
    expect(card?.getAttribute('data-highlighted')).toBe('true')
    expect(document.activeElement).toBe(card)

    button('Close dialog').click()
    await tick()
    await wrapper.find('button[aria-label="Sources"]').trigger('click')
    await tick()
    expect(document.activeElement).toBe(field('Source location'))
  })

  // spec: gui-shell/search-filters-sources-case-insensitively
  // spec: gui-shell/no-matching-source-shows-an-empty-state
  // spec: gui-shell/clearing-the-search-restores-the-list
  it('searches source names, labels, locations, and paths case-insensitively', async () => {
    const alpha = { ...dirSource, id: 'search-alpha', name: 'Named Alpha', location: '/var/Alpha', path: 'secret/alpha-path' }
    const beta = { ...dirSource, id: 'search-beta', name: 'Named Beta', location: '/var/Beta', path: 'public/beta-path' }
    const org = { ...dirSource, id: 'org-only', name: 'OrgOnly', location: '/org/records', path: 'private/org-only' }
    const { client, wrapper } = await mountSources({ userSources: [alpha, beta], organizationSources: [org] })
    await openSources(wrapper)

    const sourceIds = () => Array.from(dialog().querySelectorAll('.source-card')).map((card) => card.getAttribute('data-source-id'))
    const configurationCalls = vi.mocked(client.configuration).mock.calls.length

    for (const [query, id] of [
      ['named alpha', 'search-alpha'],
      ['VAR/ALPHA', 'search-alpha'],
      ['SECRET/ALPHA-PATH', 'search-alpha'],
      ['ORGONLY', 'org-only'],
    ] as const) {
      await setInput('Search sources', query)
      expect(sourceIds()).toEqual([id])
    }
    expect(client.configuration).toHaveBeenCalledTimes(configurationCalls)
    await setInput('Search sources', 'not found')
    expect(dialog().textContent).toContain('No sources match “not found”.')
    expect(dialog().querySelectorAll('.source-card')).toHaveLength(0)
    await setInput('Search sources', '')
    expect(sourceIds()).toEqual(['search-alpha', 'search-beta', 'org-only'])
  })
})
