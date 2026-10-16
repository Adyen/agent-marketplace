// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { event, fakeClient, mountWorkspace, row, snapshot, tick } from './test-support'

const sidebarStorageKey = 'am-gui-sidebar'
const inspectorStorageKey = 'am-gui-inspector'
const appearanceStorageKey = 'vitepress-theme-appearance'

type Mounted = ReturnType<typeof mountWorkspace>['wrapper']

async function mountShell(rows = [row('a')]) {
  const { client, push } = fakeClient()
  const { wrapper, controller } = mountWorkspace(client)
  push(event({ sequence: 1, payload: snapshot(rows) }))
  await tick()
  return { client, push, wrapper, controller }
}

function button(wrapper: Mounted, label: string) {
  return wrapper.get(`button[aria-label="${label}"]`)
}

function keydown(target: EventTarget, init: KeyboardEventInit) {
  target.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init }))
}

function setViewportWidth(width: number) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width, writable: true })
}

beforeEach(() => {
  window.localStorage.clear()
  document.documentElement.classList.remove('dark')
  setViewportWidth(1200)
})

afterEach(() => {
  setViewportWidth(1024)
  vi.restoreAllMocks()
})

describe('panel toggles', () => {
  it('folds the sidebar from its named toggle, moves the toggle to the toolbar, and persists the choice', async () => {
    const { wrapper } = await mountShell()
    const sidebar = wrapper.get('aside[aria-label="Navigation"]')
    expect(sidebar.classes()).not.toContain('gui-sidebar--folded')

    await button(wrapper, 'Toggle sidebar').trigger('click')
    await tick()

    expect(sidebar.classes()).toContain('gui-sidebar--folded')
    expect(sidebar.element.hasAttribute('inert')).toBe(true)
    expect(window.localStorage.getItem(sidebarStorageKey)).toBe('folded')
    expect(wrapper.findAll('button[aria-label="Toggle sidebar"]')).toHaveLength(1)
    expect(wrapper.get('.gui-toolbar__leading button[aria-label="Toggle sidebar"]').attributes('aria-expanded')).toBe('false')

    await button(wrapper, 'Toggle sidebar').trigger('click')
    await tick()

    expect(sidebar.classes()).not.toContain('gui-sidebar--folded')
    expect(sidebar.element.hasAttribute('inert')).toBe(false)
    expect(window.localStorage.getItem(sidebarStorageKey)).toBe('open')
    expect(button(wrapper, 'Toggle sidebar').attributes('aria-expanded')).toBe('true')
  })

  it('folds the inspector from its named toggle and restores the stored fold on mount', async () => {
    window.localStorage.setItem(inspectorStorageKey, 'folded')
    const { wrapper } = await mountShell()
    const inspector = wrapper.get('aside[aria-label="Detail"]')
    expect(inspector.classes()).toContain('gui-inspector--folded')
    expect(inspector.element.hasAttribute('inert')).toBe(true)

    await button(wrapper, 'Toggle inspector').trigger('click')
    await tick()

    expect(inspector.classes()).not.toContain('gui-inspector--folded')
    expect(window.localStorage.getItem(inspectorStorageKey)).toBe('open')
  })

  it('toggles the sidebar with ⌘B and the inspector with ⌘⌥B', async () => {
    const { wrapper } = await mountShell()
    const sidebar = wrapper.get('aside[aria-label="Navigation"]')
    const inspector = wrapper.get('aside[aria-label="Detail"]')

    keydown(document.body, { key: 'b', code: 'KeyB', metaKey: true })
    await tick()
    expect(sidebar.classes()).toContain('gui-sidebar--folded')

    keydown(document.body, { key: '∫', code: 'KeyB', metaKey: true, altKey: true })
    await tick()
    expect(inspector.classes()).toContain('gui-inspector--folded')
    expect(window.localStorage.getItem(inspectorStorageKey)).toBe('folded')
  })

  it('moves focus to the toggle when folding the panel that holds focus', async () => {
    const { wrapper } = await mountShell([row('a', { inspectable: true })])
    const sidebarSettings = button(wrapper, 'Settings').element as HTMLButtonElement
    sidebarSettings.focus()
    expect(document.activeElement).toBe(sidebarSettings)

    keydown(document.body, { key: 'b', code: 'KeyB', metaKey: true })
    await tick()
    expect(document.activeElement).toBe(button(wrapper, 'Toggle sidebar').element)

    const openResource = button(wrapper, 'Open resource').element as HTMLButtonElement
    openResource.focus()
    keydown(document.body, { key: '∫', code: 'KeyB', metaKey: true, altKey: true })
    await tick()
    expect(document.activeElement).toBe(button(wrapper, 'Toggle inspector').element)
  })

  it('starts with the inspector folded below 1000 px and dismisses its overlay with Escape', async () => {
    setViewportWidth(900)
    const { wrapper } = await mountShell()
    const inspector = wrapper.get('aside[aria-label="Detail"]')
    expect(inspector.classes()).toContain('gui-inspector--folded')

    await button(wrapper, 'Toggle inspector').trigger('click')
    await tick()
    expect(inspector.classes()).toContain('gui-inspector--overlay')
    expect(wrapper.find('.gui-scrim').exists()).toBe(true)

    keydown(document.body, { key: 'Escape' })
    await tick()
    expect(inspector.classes()).toContain('gui-inspector--folded')
    expect(wrapper.find('.gui-scrim').exists()).toBe(false)
  })
})

describe('appearance control', () => {
  async function chooseAppearance(value: 'system' | 'light' | 'dark') {
    const input = document.querySelector<HTMLInputElement>(`input[name="am-appearance"][value="${value}"]`)
    expect(input).not.toBeNull()
    input!.checked = true
    input!.dispatchEvent(new Event('change', { bubbles: true }))
    await tick()
  }

  it('System, Light, and Dark set and clear the dark class and the stored choice', async () => {
    const { wrapper } = await mountShell()
    await button(wrapper, 'Settings').trigger('click')
    await tick()

    await chooseAppearance('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(window.localStorage.getItem(appearanceStorageKey)).toBe('dark')

    await chooseAppearance('light')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(window.localStorage.getItem(appearanceStorageKey)).toBe('light')

    await chooseAppearance('dark')
    await chooseAppearance('system')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(window.localStorage.getItem(appearanceStorageKey)).toBeNull()
  })
})

describe('sidebar navigation', () => {
  it('Pending sync and group rows select and bring their group into view', async () => {
    const scrollIntoView = vi.spyOn(HTMLElement.prototype, 'scrollIntoView').mockImplementation(() => undefined)
    const { wrapper } = await mountShell([row('a'), row('p', { group: 'pending', status: 'pending' })])
    const pendingSection = () => wrapper.get('button.workspace__group-toggle[aria-label="Pending sync"]')
    expect(pendingSection().attributes('aria-current')).toBeUndefined()

    await wrapper.get('button[aria-label="Pending sync group"]').trigger('click')
    await tick()
    expect(pendingSection().attributes('aria-current')).toBe('true')
    expect(scrollIntoView).toHaveBeenCalled()

    await wrapper.get('button[aria-label="Show Enabled group"]').trigger('click')
    await tick()
    expect(wrapper.get('button.workspace__group-toggle[aria-label="Enabled"]').attributes('aria-current')).toBe('true')
    expect(pendingSection().attributes('aria-current')).toBeUndefined()
  })

  it('the Search row opens the search field', async () => {
    const { wrapper } = await mountShell()
    expect(wrapper.find('input[aria-label="Search resources"]').exists()).toBe(false)

    await wrapper.get('button[aria-label="Open search"]').trigger('click')
    await tick()

    expect(wrapper.find('input[aria-label="Search resources"]').exists()).toBe(true)
  })
})

describe('keyboard shortcuts', () => {
  it('⌘K opens the search field', async () => {
    const { wrapper } = await mountShell()
    keydown(document.body, { key: 'k', metaKey: true })
    await tick()
    expect(wrapper.find('input[aria-label="Search resources"]').exists()).toBe(true)
  })

  it('s syncs only from the resource list, not from a text input or while a modal is open', async () => {
    const { client, wrapper } = await mountShell()
    const action = vi.mocked(client.action)
    const syncCalls = () => action.mock.calls.filter(([name]) => name === 'sync').length

    const option = wrapper.get('[role="option"]').element as HTMLElement
    option.focus()
    keydown(option, { key: 's' })
    await tick()
    expect(syncCalls()).toBe(1)

    keydown(document.body, { key: '/' })
    await tick()
    const input = wrapper.get('input[aria-label="Search resources"]').element as HTMLInputElement
    input.focus()
    keydown(input, { key: 's' })
    await tick()
    expect(syncCalls()).toBe(1)

    await button(wrapper, 'Settings').trigger('click')
    await tick()
    keydown(option, { key: 's' })
    await tick()
    expect(syncCalls()).toBe(1)
  })

  it('Update is a visible button separate from Sync', async () => {
    const { client, wrapper } = await mountShell()
    const sync = button(wrapper, 'Sync').element
    const update = button(wrapper, 'Update').element
    expect(update).not.toBe(sync)
    expect(update.parentElement).toBe(sync.parentElement)

    await button(wrapper, 'Update').trigger('click')
    await tick()
    expect(client.action).toHaveBeenCalledWith('update')
  })
})
