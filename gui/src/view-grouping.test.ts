// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { event, fakeClient, mountWorkspace, row, snapshot, tick } from './test-support'
import { VIEW_STORAGE_KEY } from './grouping'

const LOCAL = { id: 'local', label: '(local)' }
const TEAM_MAIN = { id: 'https://example.com/team.git\x01main\x01', label: 'team' }
const TEAM_RELEASE = { id: 'https://example.com/team.git\x01release\x01', label: 'team' }

const alpha = row('alpha', { kind: 'skill', group: 'enabled', status: 'enabled', source: TEAM_MAIN, registry: 'team' })
const bravo = row('bravo', { kind: 'command', group: 'enabled', status: 'enabled', source: LOCAL })
const charlie = row('charlie', { kind: 'skill', group: 'pending', status: 'enabled (pending sync)', source: TEAM_RELEASE, registry: 'team' })

type Wrapper = ReturnType<typeof mountWorkspace>['wrapper']

function optionFor(wrapper: Wrapper, id: string) {
  return wrapper.get(`[role="option"][id="row-${id}"]`)
}

function sectionToggle(wrapper: Wrapper, title: string) {
  return wrapper.get(`button.workspace__group-toggle[aria-label="${title}"]`)
}

function viewButton(wrapper: Wrapper) {
  return wrapper.get('button[aria-label="View"]')
}

async function openView(wrapper: Wrapper) {
  if (!wrapper.find('.gui-view-panel').exists()) {
    await viewButton(wrapper).trigger('click')
  }
}

async function chooseGroupBy(wrapper: Wrapper, value: string) {
  await openView(wrapper)
  await wrapper.get(`input[name="view-group-by"][value="${value}"]`).setValue(true)
  await tick()
}

async function chooseThenBy(wrapper: Wrapper, value: string) {
  await openView(wrapper)
  await wrapper.get(`input[name="view-then-by"][value="${value}"]`).setValue(true)
  await tick()
}

function checkedValue(wrapper: Wrapper, name: string): string | undefined {
  const checked = wrapper.findAll(`input[name="${name}"]`).find((input) => (input.element as HTMLInputElement).checked)
  return checked?.attributes('value')
}

function optionValues(wrapper: Wrapper, name: string): string[] {
  return wrapper.findAll(`input[name="${name}"]`).map((input) => input.attributes('value') ?? '')
}

function requestCount(client: ReturnType<typeof fakeClient>['client']): number {
  return Object.values(client).reduce((total, member) => total + (vi.isMockFunction(member) ? member.mock.calls.length : 0), 0)
}

function keydown(target: EventTarget, init: KeyboardEventInit) {
  target.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init }))
}

async function mountWith(rows = [alpha, bravo, charlie]) {
  const fake = fakeClient()
  const mounted = mountWorkspace(fake.client)
  fake.push(event({ sequence: 1, payload: snapshot(rows) }))
  await tick()
  return { ...fake, ...mounted }
}

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('View menu', () => {
  it('opens from the named button and from the v key, focusing the current choice', async () => {
    const { wrapper } = await mountWith()
    expect(viewButton(wrapper).attributes('aria-haspopup')).toBe('true')
    expect(viewButton(wrapper).attributes('aria-expanded')).toBe('false')

    await viewButton(wrapper).trigger('click')
    expect(viewButton(wrapper).attributes('aria-expanded')).toBe('true')
    expect(wrapper.findAll('fieldset legend').map((legend) => legend.text())).toEqual(['Group by', 'Then by'])
    expect(document.activeElement).toBe(wrapper.get('input[name="view-group-by"][value="status"]').element)

    await wrapper.get('.gui-view-panel').trigger('keydown', { key: 'Escape' })
    await tick()
    expect(wrapper.find('.gui-view-panel').exists()).toBe(false)
    expect(document.activeElement).toBe(viewButton(wrapper).element)

    keydown(document.body, { key: 'v' })
    await tick()
    expect(wrapper.find('.gui-view-panel').exists()).toBe(true)
  })

  it('closes on an outside pointer press without pulling focus back to the View button', async () => {
    const { wrapper } = await mountWith()
    await openView(wrapper)
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await tick()
    expect(wrapper.find('.gui-view-panel').exists()).toBe(false)
    expect(document.activeElement).not.toBe(viewButton(wrapper).element)
  })

  it('ignores the v key while typing in search', async () => {
    const { wrapper } = await mountWith()
    await wrapper.get('button[aria-label="Search"]').trigger('click')
    const input = wrapper.get('#workspace-search').element as HTMLInputElement
    keydown(input, { key: 'v' })
    await tick()
    expect(wrapper.find('.gui-view-panel').exists()).toBe(false)
  })

  it('offers None, Kind, and Source as Then by and resets Then by when the primary axis matches it', async () => {
    const { wrapper } = await mountWith()
    await openView(wrapper)
    expect(optionValues(wrapper, 'view-then-by')).toEqual(['none', 'kind', 'source'])

    await chooseThenBy(wrapper, 'kind')
    expect(checkedValue(wrapper, 'view-then-by')).toBe('kind')

    await chooseGroupBy(wrapper, 'kind')
    expect(optionValues(wrapper, 'view-then-by')).toEqual(['none', 'source'])
    expect(checkedValue(wrapper, 'view-group-by')).toBe('kind')
    expect(checkedValue(wrapper, 'view-then-by')).toBe('none')
  })

  // spec: gui-shell/every-valid-grouping-combination-retains-distinct-resources
  it('renders nested sections for the chosen grouping and lists top-level sections in the sidebar', async () => {
    const { wrapper } = await mountWith()
    await chooseGroupBy(wrapper, 'kind')
    await chooseThenBy(wrapper, 'source')

    expect(wrapper.findAll('h2 button.workspace__group-toggle').map((toggle) => toggle.attributes('aria-label'))).toEqual([
      'Needs attention', 'Skills', 'Commands',
    ])
    const nested = wrapper.findAll('h3 button.workspace__group-toggle').map((toggle) => toggle.attributes('aria-label'))
    expect(nested).toEqual(['team', '(local)'])
    expect(wrapper.findAll('ul[aria-label="Resources: Needs attention"] [role="option"]')).toHaveLength(1)
    expect(wrapper.findAll('ul[aria-label="Resources: team"] [role="option"]')).toHaveLength(1)
    expect(wrapper.find('ul[aria-label="Resources: Skills"]').exists()).toBe(false)

    const sidebarGroups = wrapper.findAll('section[aria-label="Groups"] button').map((button) => button.attributes('aria-label'))
    expect(sidebarGroups).toEqual(['Show Needs attention group', 'Show Skills group', 'Show Commands group'])
  })

  // spec: gui-shell/grouping-choice-persists-locally-without-workspace-writes
  it('persists the choice in local storage without sending a request', async () => {
    const { wrapper, client } = await mountWith()
    const before = requestCount(client)
    await chooseGroupBy(wrapper, 'source')
    await chooseThenBy(wrapper, 'kind')
    expect(requestCount(client)).toBe(before)
    expect(window.localStorage.getItem(VIEW_STORAGE_KEY)).toBe('{"version":1,"groupBy":"source","thenBy":"kind"}')
  })

  // spec: gui-shell/grouping-choice-persists-locally-without-workspace-writes
  it('restores the stored choice on mount', async () => {
    window.localStorage.setItem(VIEW_STORAGE_KEY, '{"version":1,"groupBy":"source","thenBy":"kind"}')
    const { wrapper } = await mountWith()
    await openView(wrapper)
    expect(checkedValue(wrapper, 'view-group-by')).toBe('source')
    expect(checkedValue(wrapper, 'view-then-by')).toBe('kind')
  })

  // spec: gui-shell/grouping-choice-persists-locally-without-workspace-writes
  it('falls back to Status and None when the stored choice is malformed', async () => {
    window.localStorage.setItem(VIEW_STORAGE_KEY, '{"version":1,"groupBy":"status","thenBy":"status"}')
    const { wrapper } = await mountWith()
    await openView(wrapper)
    expect(checkedValue(wrapper, 'view-group-by')).toBe('status')
    expect(checkedValue(wrapper, 'view-then-by')).toBe('none')
  })

  // spec: gui-shell/grouping-changes-preserve-folds-and-selection
  it('keeps a selected resource selected when it stays visible', async () => {
    const { wrapper } = await mountWith()
    await optionFor(wrapper, alpha.id).trigger('click')
    await chooseGroupBy(wrapper, 'kind')
    expect(optionFor(wrapper, alpha.id).attributes('aria-selected')).toBe('true')
  })

  // spec: gui-shell/grouping-changes-preserve-folds-and-selection
  it('selects the deepest visible enclosing section when the selected resource is folded away', async () => {
    const { wrapper } = await mountWith()
    await chooseGroupBy(wrapper, 'kind')
    await sectionToggle(wrapper, 'Skills').trigger('click')
    await tick()
    await chooseGroupBy(wrapper, 'status')
    await optionFor(wrapper, alpha.id).trigger('click')
    await tick()

    await chooseGroupBy(wrapper, 'kind')
    expect(sectionToggle(wrapper, 'Skills').attributes('aria-current')).toBe('true')
    expect(wrapper.find(`[role="option"][id="row-${alpha.id}"]`).exists()).toBe(false)
  })

  // spec: gui-shell/grouping-changes-preserve-folds-and-selection
  it('restores each grouping combination its own folds when switching back', async () => {
    const { wrapper } = await mountWith()
    await chooseGroupBy(wrapper, 'kind')
    await sectionToggle(wrapper, 'Skills').trigger('click')
    await tick()
    expect(sectionToggle(wrapper, 'Skills').attributes('aria-expanded')).toBe('false')

    await chooseGroupBy(wrapper, 'status')
    expect(sectionToggle(wrapper, 'Enabled').attributes('aria-expanded')).toBe('true')
    expect(wrapper.findAll('[role="option"]').length).toBeGreaterThan(0)

    await chooseGroupBy(wrapper, 'kind')
    expect(sectionToggle(wrapper, 'Skills').attributes('aria-expanded')).toBe('false')
    expect(wrapper.find(`ul[aria-label="Resources: Skills"]`).exists()).toBe(false)
  })

  // spec: gui-shell/grouping-changes-preserve-folds-and-selection
  it('keeps browsing folds through a search and restores them when the search clears', async () => {
    const { wrapper } = await mountWith()
    await chooseGroupBy(wrapper, 'kind')
    await sectionToggle(wrapper, 'Skills').trigger('click')
    await tick()

    await wrapper.get('button[aria-label="Search"]').trigger('click')
    await wrapper.get('#workspace-search').setValue('alpha')
    await wrapper.get('#workspace-search').trigger('keydown', { key: 'Enter' })
    await tick()
    expect(wrapper.find(`[role="option"][id="row-${alpha.id}"]`).exists()).toBe(true)

    await wrapper.get('button[aria-label="Clear search"]').trigger('click')
    await tick()
    expect(sectionToggle(wrapper, 'Skills').attributes('aria-expanded')).toBe('false')
    expect(wrapper.find(`[role="option"][id="row-${alpha.id}"]`).exists()).toBe(false)
  })
})

describe('redundant columns and problem reasons', () => {
  const broken = row('broken-skill', { group: 'broken', status: 'broken: missing SKILL.md', checked: false, source: TEAM_MAIN, registry: 'team' })
  const stale = row('stale-skill', { group: 'stale', status: 'stale', source: TEAM_MAIN, registry: 'team' })

  it('hides the registry column for a source axis and the kind column for a kind axis', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([alpha, bravo, charlie]) }))
    await tick()

    const registry = () => optionFor(wrapper, alpha.id).get('.workspace__registry')
    const kind = () => optionFor(wrapper, alpha.id).get('.workspace__kind')
    const columns = () => (optionFor(wrapper, alpha.id).element as HTMLElement).style.gridTemplateColumns

    expect(registry().classes()).not.toContain('gui-visually-hidden')
    expect(kind().classes()).not.toContain('gui-visually-hidden')
    expect(columns()).toContain('24ch')

    await chooseGroupBy(wrapper, 'source')
    expect(registry().classes()).toContain('gui-visually-hidden')
    expect(registry().text()).toBe('team')
    expect(kind().classes()).not.toContain('gui-visually-hidden')
    expect(columns()).not.toContain('24ch')
    expect(columns()).toContain('10ch')

    await chooseGroupBy(wrapper, 'status')
    await chooseThenBy(wrapper, 'source')
    expect(registry().classes()).toContain('gui-visually-hidden')

    await chooseThenBy(wrapper, 'none')
    await chooseGroupBy(wrapper, 'kind')
    expect(kind().classes()).toContain('gui-visually-hidden')
    expect(registry().classes()).not.toContain('gui-visually-hidden')
    expect(columns()).not.toContain('10ch')
    expect(columns()).toContain('24ch')
  })

  it('tags broken, malformed, and stale rows with their reason in the tone of their group', async () => {
    const malformed = row('malformed-skill', { group: 'malformed', status: 'malformed: bad frontmatter', checked: false })
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([broken, malformed, stale, alpha]) }))
    await tick()

    const reason = (id: string) => optionFor(wrapper, id).find('.workspace__reason')
    expect(reason(broken.id).text()).toBe('missing SKILL.md')
    expect(reason(broken.id).classes()).toContain('workspace__reason--danger')
    expect(reason(malformed.id).text()).toBe('bad frontmatter')
    expect(reason(malformed.id).classes()).toContain('workspace__reason--warning')
    expect(reason(stale.id).text()).toBe('stale')
    expect(reason(stale.id).classes()).toContain('workspace__reason--warning')
    expect(reason(alpha.id).exists()).toBe(false)
  })
})
