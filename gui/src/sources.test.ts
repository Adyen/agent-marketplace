// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import type { mount } from '@vue/test-utils'
import { RequestError, type Configuration, type OperationRef } from './api/client'
import { configuration, event, fakeClient, mountWorkspace, snapshot, tick } from './test-support'

const dirSource = {
  id: 'user-dir-id',
  kind: 'directory' as const,
  location: '/var/regs',
  ref: '',
  path: '',
  sourceHarness: '',
}
const gitSource = {
  id: 'user-git-id',
  kind: 'git' as const,
  location: 'https://example.com/repo.git',
  ref: 'main',
  path: '',
  sourceHarness: '',
}
const marketplaceSource = {
  id: 'user-mp-id',
  kind: 'marketplace' as const,
  location: 'https://marketplace.example',
  ref: '',
  path: '',
  sourceHarness: '',
}

const sourcesConfiguration = (overrides: Partial<Configuration> = {}): Configuration =>
  configuration({ userSources: [dirSource, gitSource, marketplaceSource], ...overrides })

const sourcesClient = (overrides: Partial<Configuration> = {}) =>
  fakeClient({ configuration: sourcesConfiguration(overrides) })

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

function field(label: string): HTMLElement {
  const el = dialog().querySelector<HTMLElement>(`input[aria-label="${label}"], select[aria-label="${label}"]`)
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
  const input = field(label) as HTMLInputElement
  input.value = value
  input.dispatchEvent(new Event('input'))
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

describe('Source settings', () => {
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

  it('offers no removal control for marketplace sources', async () => {
    const { client, push } = sourcesClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    expect(() => button(`Remove source ${marketplaceSource.location}`)).toThrow()
    expect(client.configure).not.toHaveBeenCalled()
  })

  it('keeps organization sources free of any source controls', async () => {
    const { client, push } = sourcesClient({
      organizationSources: [{ id: 'org-id', kind: 'directory', location: '/opt/org', ref: '', path: '', sourceHarness: '' }],
    })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    const org = dialog().querySelector('[aria-label="Organization sources"]')!
    expect(org.querySelectorAll('button, input, select, a')).toHaveLength(0)
    expect(client.configure).not.toHaveBeenCalled()
  })

  it('adds a directory source with a typed body', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
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
  })

  it('adds a git source with an explicit ref and optional subpath', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    const kindSelect = field('Source kind') as HTMLSelectElement
    kindSelect.value = 'git'
    kindSelect.dispatchEvent(new Event('change'))
    await tick()
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

    const kindSelect = field('Source kind') as HTMLSelectElement
    kindSelect.value = 'git'
    kindSelect.dispatchEvent(new Event('change'))
    await tick()
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
  })

  it('cancelling the dialog discards the draft and sends no request', async () => {
    const { client, push } = sourcesClient({ userSources: [] })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1 }))
    await tick()

    await openSources(wrapper)
    await setInput('Source location', '/draft')
    button('Cancel').click()
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
    const kindSelect = field('Source kind') as HTMLSelectElement
    kindSelect.value = 'git'
    kindSelect.dispatchEvent(new Event('change'))
    await tick()
    await setInput('Source location', 'https://example.com/reg.git')
    await setInput('Source ref', 'main')
    button(`Remove source ${dirSource.location}`).click()
    await tick()
    expect(dialog().querySelector('[aria-label="Confirm removal"]')).not.toBeNull()

    button('Cancel').click()
    await tick()
    await openSources(wrapper)

    expect((field('Source kind') as HTMLSelectElement).value).toBe('directory')
    expect((field('Source location') as HTMLInputElement).value).toBe('')
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
    expect((field('Source location') as HTMLInputElement).value).toBe('/opt/new-regs')
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
    expect((field('Source location') as HTMLInputElement).value).toBe('/x')
    button('Add source').click()
    await tick()
    expect(client.configure).toHaveBeenCalledTimes(2)
  })
})
