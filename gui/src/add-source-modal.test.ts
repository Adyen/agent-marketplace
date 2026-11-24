// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { RequestError } from './api/client'
import { event, fakeClient, mountWorkspace, snapshot, tick } from './test-support'

function modal(): HTMLElement {
  const el = document.body.querySelector<HTMLElement>('.gui-modal__frame[aria-label="Add source"]')
  if (el == null) {
    throw new Error('add source modal not open')
  }
  return el
}

// spec: gui-shell/add-source-modal-opens-from-the-sidebar-plus
async function previewAndAdd() {
  ;(modal().querySelector('button[aria-label="Preview source"]') as HTMLButtonElement).click()
  await tick()
  ;(modal().querySelector('button[aria-label="Add source"]') as HTMLButtonElement).click()
  await tick()
}

describe('add source modal', () => {
  it('opens from the sidebar plus with the source kinds and no inline Settings form', async () => {
    const { client, push } = fakeClient()
    const { wrapper: view } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([]) }))
    await tick()

    view.get('button[aria-label="Sources"]').trigger('click')
    await tick()

    expect(modal().querySelector('h2')?.textContent).toBe('Add source')
    const kinds = Array.from(modal().querySelectorAll('input[name="source-kind"]')).map((input) => (input as HTMLInputElement).value)
    expect(kinds).toEqual(['directory', 'git', 'marketplace'])
  })

  // spec: gui-shell/add-source-modal-stays-open-while-the-add-is-pending
  it('stays open with a busy add button until the operation settles', async () => {
    const { client, push } = fakeClient()
    vi.mocked(client.configure).mockResolvedValueOnce({ id: 9, kind: 'mutation' })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([]) }))
    await tick()

    wrapper.get('button[aria-label="Sources"]').trigger('click')
    await tick()
    const input = modal().querySelector<HTMLInputElement>('input[aria-label="Source location"]')!
    input.value = '/opt/new-regs'
    input.dispatchEvent(new Event('input'))
    await tick()
    await previewAndAdd()

    const busy = modal().querySelector<HTMLButtonElement>('button[aria-label="Adding source"]')
    expect(busy?.textContent?.trim()).toBe('Adding…')
    expect(busy?.disabled).toBe(true)
    expect(Array.from(modal().querySelectorAll('button')).find((button) => button.textContent?.trim() === 'Cancel')?.disabled).toBe(true)
    expect(modal()).not.toBeNull()
  })

  // spec: gui-shell/add-source-modal-stays-open-on-a-failed-add
  it('keeps the modal open and shows the error inline when adding fails', async () => {
    const { client, push } = fakeClient()
    vi.mocked(client.configure).mockRejectedValueOnce(new RequestError(502, 'localapi.source_unavailable'))
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([]) }))
    await tick()

    wrapper.get('button[aria-label="Sources"]').trigger('click')
    await tick()
    const input = modal().querySelector<HTMLInputElement>('input[aria-label="Source location"]')!
    input.value = '/opt/new-regs'
    input.dispatchEvent(new Event('input'))
    await tick()
    await previewAndAdd()

    expect(modal()).not.toBeNull()
    expect(modal().querySelector('[role="alert"]')).not.toBeNull()
  })

  // spec: gui-shell/cancel-and-enter-in-add-source
  it('submits with Enter in the location field and closes without adding on Cancel', async () => {
    const { client, push } = fakeClient()
    vi.mocked(client.configure).mockResolvedValueOnce({ id: 9, kind: 'mutation' })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([]) }))
    await tick()

    wrapper.get('button[aria-label="Sources"]').trigger('click')
    await tick()
    const cancel = Array.from(modal().querySelectorAll('button')).find((button) => button.textContent?.trim() === 'Cancel')!
    cancel.click()
    await tick()
    expect(document.body.querySelector('.gui-modal__frame[aria-label="Add source"]')).toBeNull()
    expect(client.configure).not.toHaveBeenCalled()

    wrapper.get('button[aria-label="Sources"]').trigger('click')
    await tick()
    const input = modal().querySelector<HTMLInputElement>('input[aria-label="Source location"]')!
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await tick()
    expect(client.configure).not.toHaveBeenCalled()

    input.value = '/opt/new-regs'
    input.dispatchEvent(new Event('input'))
    await tick()
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await tick()
    expect(client.sourcePreview).toHaveBeenCalledWith({ kind: 'directory', location: '/opt/new-regs' }, expect.anything())
    expect(modal().querySelector('[aria-label="Resources found"]')?.textContent).toContain('solo')
    expect(client.configure).not.toHaveBeenCalled()
    ;(modal().querySelector('button[aria-label="Add source"]') as HTMLButtonElement).click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({ action: 'add-source', source: { kind: 'directory', location: '/opt/new-regs' } })
  })

  // spec: gui-shell/add-source-previews-a-git-source-before-adding
  it('previews a Git source at its default branch, then adds it at the resolved ref', async () => {
    const { client, push } = fakeClient()
    vi.mocked(client.configure).mockResolvedValueOnce({ id: 9, kind: 'mutation' })
    vi.mocked(client.sourcePreview).mockResolvedValueOnce({
      location: 'https://example.com/repo.git',
      ref: 'trunk',
      path: '',
      commit: '0123456789abcdef0123456789abcdef01234567',
      resources: [
        { kind: 'skill', name: 'review', description: 'Reviews code' },
        { kind: 'skill', name: 'broken', description: '', problem: 'missing SKILL.md' },
        { kind: 'command', name: 'ship', description: '' },
      ],
      bundles: [],
      warnings: [],
    })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([]) }))
    await tick()
    wrapper.get('button[aria-label="Sources"]').trigger('click')
    await tick()

    modal().querySelector<HTMLInputElement>('input[name="source-kind"][value="git"]')!.click()
    await tick()
    const input = modal().querySelector<HTMLInputElement>('input[aria-label="Source location"]')!
    input.value = 'https://example.com/repo.git'
    input.dispatchEvent(new Event('input'))
    await tick()
    ;(modal().querySelector('button[aria-label="Preview source"]') as HTMLButtonElement).click()
    await tick()

    expect(client.sourcePreview).toHaveBeenCalledWith({ kind: 'git', location: 'https://example.com/repo.git' }, expect.anything())
    const preview = modal().querySelector('[aria-label="Source preview"]')!
    const summary = preview.querySelector('.add-source-modal__summary')!
    expect(summary.textContent?.replace(/\s+/g, ' ')).toContain('2 resources at 0123456789ab on trunk')
    expect(summary.querySelector('.add-source-modal__counts')?.textContent?.trim()).toBe('1 skill, 1 command')
    expect(preview.textContent).toContain("Can't be read: missing SKILL.md")

    ;(modal().querySelector('button[aria-label="Add source"]') as HTMLButtonElement).click()
    await tick()
    expect(client.configure).toHaveBeenCalledWith({
      action: 'add-source',
      source: { kind: 'git', location: 'https://example.com/repo.git', ref: 'trunk' },
    })
  })

  // spec: gui-shell/add-source-preview-installs-the-picked-resources
  it('installs the resources turned on in the preview along with the source', async () => {
    const { client, push } = fakeClient()
    vi.mocked(client.configure).mockResolvedValueOnce({ id: 9, kind: 'mutation' })
    vi.mocked(client.sourcePreview).mockResolvedValueOnce({
      location: 'https://example.com/repo.git',
      ref: 'main',
      path: '',
      commit: '0123456789abcdef0123456789abcdef01234567',
      resources: [
        { kind: 'skill', name: 'review', description: 'Reviews code' },
        { kind: 'skill', name: 'broken', description: '', problem: 'missing SKILL.md' },
        { kind: 'command', name: 'ship', description: '' },
      ],
      bundles: [],
      warnings: [],
    })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([]) }))
    await tick()
    wrapper.get('button[aria-label="Sources"]').trigger('click')
    await tick()
    modal().querySelector<HTMLInputElement>('input[name="source-kind"][value="git"]')!.click()
    await tick()
    const input = modal().querySelector<HTMLInputElement>('input[aria-label="Source location"]')!
    input.value = 'https://example.com/repo.git'
    input.dispatchEvent(new Event('input'))
    await tick()
    ;(modal().querySelector('button[aria-label="Preview source"]') as HTMLButtonElement).click()
    await tick()

    const switches = () => Array.from(modal().querySelectorAll<HTMLButtonElement>('[aria-label="Resources found"] [role="switch"]'))
    expect(switches().map((el) => el.getAttribute('aria-label'))).toEqual(['Install Skill review', 'Install Command ship'])
    expect(switches().every((el) => el.getAttribute('aria-checked') === 'false')).toBe(true)
    expect(modal().querySelector('.add-source-modal__confirm')?.textContent?.trim()).toBe('Add source')

    modal().querySelector<HTMLButtonElement>('[role="switch"][aria-label="Install all resources"]')!.click()
    await tick()
    expect(modal().querySelector('.add-source-modal__confirm')?.textContent?.trim()).toBe('Add source and install 2')
    switches()[1].click()
    await tick()
    const confirm = modal().querySelector<HTMLButtonElement>('.add-source-modal__confirm')!
    expect(confirm.textContent?.trim()).toBe('Add source and install 1')
    confirm.click()
    await tick()

    expect(client.configure).toHaveBeenCalledWith({
      action: 'add-source',
      source: { kind: 'git', location: 'https://example.com/repo.git', ref: 'main' },
      install: [{ kind: 'skill', name: 'review' }],
      commit: '0123456789abcdef0123456789abcdef01234567',
    })
  })

  // spec: gui-shell/add-source-preview-failure-stays-on-the-form
  it('keeps the form and explains a source that cannot be loaded', async () => {
    const { client, push } = fakeClient()
    vi.mocked(client.sourcePreview).mockResolvedValueOnce({
      location: '/opt/new-regs', ref: '', path: '', commit: '', resources: [], bundles: [], warnings: [],
      failure: 'fatal: could not resolve host: example.invalid',
    })
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([]) }))
    await tick()
    wrapper.get('button[aria-label="Sources"]').trigger('click')
    await tick()
    const input = modal().querySelector<HTMLInputElement>('input[aria-label="Source location"]')!
    input.value = '/opt/new-regs'
    input.dispatchEvent(new Event('input'))
    await tick()
    ;(modal().querySelector('button[aria-label="Preview source"]') as HTMLButtonElement).click()
    await tick()

    expect(modal().querySelector('[aria-label="Source preview"]')).toBeNull()
    expect(modal().querySelector('[role="alert"]')?.textContent).toContain("Couldn't reach the repository")
    expect(modal().querySelector('.add-source-modal__detail')?.textContent).toBe('fatal: could not resolve host: example.invalid')
    expect(input.value).toBe('/opt/new-regs')
    expect(client.configure).not.toHaveBeenCalled()
  })
})
