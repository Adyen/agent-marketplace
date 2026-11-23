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
    ;(modal().querySelector('button[aria-label="Add source"]') as HTMLButtonElement).click()
    await tick()

    const busy = modal().querySelector<HTMLButtonElement>('button[aria-label="Adding source"]')
    expect(busy?.textContent?.trim()).toBe('Adding…')
    expect(busy?.disabled).toBe(true)
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
    ;(modal().querySelector('button[aria-label="Add source"]') as HTMLButtonElement).click()
    await tick()

    expect(modal()).not.toBeNull()
    expect(modal().querySelector('[role="alert"]')).not.toBeNull()
  })
})
