// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { event, fakeClient, mountWorkspace, row, snapshot, tick } from './test-support'

type WorkspaceWrapper = ReturnType<typeof mountWorkspace>['wrapper']

function optionFor(wrapper: WorkspaceWrapper, id: string) {
  const option = wrapper.findAll('[role="option"]').find((entry) => entry.attributes('id') === `row-${id}`)
  if (option == null) {
    throw new Error(`missing resource option ${id}`)
  }
  return option
}

function groupButton(wrapper: WorkspaceWrapper, title: string) {
  const button = wrapper.findAll('.workspace__group-toggle').find((entry) => entry.attributes('aria-label') === title)
  if (button == null) {
    throw new Error(`missing status group ${title}`)
  }
  return button
}

async function openSearch(wrapper: WorkspaceWrapper) {
  const button = wrapper.find('button[aria-label="Search"]')
  expect(button.exists()).toBe(true)
  await button.trigger('click')
  await tick()
  const input = wrapper.find('input[aria-label="Search resources"]')
  expect(input.exists()).toBe(true)
  return input
}

describe('local resource search', () => {
  // spec: session-navigation/ranked-search-within-sections
  // spec: session-navigation/name-match-outranks-metadata-match
  it('ranks fuzzy names within leaves and selects the best match across sections', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([
      row('zeta', { id: 'row-zeta', group: 'enabled', searchWords: ['readme', 'zeta'] }),
      row('readme-helper', { id: 'row-helper', group: 'enabled' }),
      row('readme', { id: 'row-z', group: 'enabled' }),
      row('readme', { id: 'row-a', group: 'enabled' }),
      row('required-meta', { id: 'row-required', group: 'required', required: true, searchWords: ['readme', 'required-meta'] }),
      row('readme', { id: 'row-0', group: 'available', checked: false }),
    ]) }))
    await tick()

    const input = await openSearch(wrapper)
    await input.setValue('readme')
    await tick()

    expect(wrapper.findAll('.workspace__group-toggle').map((button) => button.attributes('aria-label'))).toEqual([
      'Required', 'Enabled', 'Available',
    ])
    expect(wrapper.findAll('ul[aria-label="Resources: Enabled"] [role="option"]').map((option) => option.attributes('id'))).toEqual([
      'row-row-a', 'row-row-z', 'row-row-helper', 'row-row-zeta',
    ])
    expect(wrapper.find('[role="option"][aria-selected="true"]')?.attributes('id')).toBe('row-row-0')
  })

  // spec: session-navigation/scattered-metadata-letters
  it('does not fuzzy-match letters scattered across metadata words', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([
      row('unrelated', {
        id: 'row-unrelated',
        description: 'Horrible little markers',
        searchWords: ['horrible', 'little', 'markers', 'unrelated'],
      }),
      row('helm', { id: 'row-helm' }),
    ]) }))
    await tick()

    const input = await openSearch(wrapper)
    await input.setValue('hlm')
    await tick()

    expect(wrapper.findAll('[role="option"]').map((option) => option.attributes('id'))).toEqual(['row-row-helm'])
  })

  // spec: session-navigation/metadata-only-match
  it('requires every metadata query term to prefix a word and leaves metadata-only names unhighlighted', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([
      row('platform', { id: 'row-platform', searchWords: ['git', 'platform'] }),
      row('partial', { id: 'row-partial', searchWords: ['git', 'other'] }),
    ]) }))
    await tick()

    const input = await openSearch(wrapper)
    await input.setValue('PLAT GIT')
    await tick()

    expect(wrapper.findAll('[role="option"]').map((option) => option.attributes('id'))).toEqual(['row-row-platform'])
    expect(wrapper.findAll('[role="option"] mark')).toHaveLength(0)
  })

  // spec: session-rendering-safety/hostile-text-is-inert
  // spec: session-navigation/metadata-only-match
  it('renders sanitized hostile names literally and highlights only matched name characters', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    const rawName = '<img src=x onerror=alert(1)>\u001b[31m\u202e'
    const displayName = '<img src=x onerror=alert(1)>\\x1B[31m\\u202E'
    const registryDisplay = 'registry <script>\\x1B[31m\\u202E Å'
    const descriptionDisplay = 'description <img src=x>\nprintable Ω\\x1B[31m\\u202E'
    const statusDisplay = 'malformed: bad <b>frontmatter</b> \\x1B[31m\\u202E'
    const harnessDisplay = 'custom <img>\\x1B[31m\\u202E'
    push(event({ sequence: 1, payload: snapshot([
      row('hostile', {
        id: 'row-hostile',
        name: rawName,
        displayName,
        searchName: displayName.toLowerCase(),
        searchWords: ['img', 'src', 'x', 'onerror', 'alert', '1'],
        registry: registryDisplay,
        description: descriptionDisplay,
        status: statusDisplay,
        harnesses: [harnessDisplay],
      }),
      row('metadata-only', {
        id: 'row-metadata',
        description: 'img is only in metadata',
        searchWords: ['img', 'metadata', 'only'],
      }),
      row('supplementary', {
        id: 'row-supplementary',
        name: '\u{10400}',
        displayName: '\u{10400}',
        searchName: '\u{10428}',
        searchWords: ['\u{10428}'],
      }),
      row('dotted-i', {
        id: 'row-idot',
        name: 'İ',
        displayName: 'İ',
        searchName: 'i',
        searchWords: ['i'],
      }),
    ]) }))
    await tick()

    const input = await openSearch(wrapper)
    await input.setValue('img')
    await tick()

    expect(wrapper.findAll('img, script, b, [onerror], [onload]')).toHaveLength(0)
    const hostileOption = optionFor(wrapper, 'row-hostile')
    expect(hostileOption.find('.workspace__name').text()).toBe(displayName)
    expect(hostileOption.find('.workspace__kind').text()).toBe('skill')
    expect(hostileOption.find('.workspace__registry').text()).toBe(registryDisplay)
    expect(hostileOption.findAll('mark').map((mark) => mark.text()).join('')).toBe('img')
    expect(optionFor(wrapper, 'row-metadata').findAll('mark')).toHaveLength(0)
    const detailText = wrapper.find('[aria-label="Detail"]').text()
    expect(detailText).toContain(displayName)
    expect(detailText).toContain(registryDisplay)
    expect(detailText).toContain(descriptionDisplay)
    expect(detailText).toContain(statusDisplay)
    expect(detailText).toContain(harnessDisplay)
    expect(detailText).not.toContain('\u001b')
    expect(detailText).not.toContain('\u202e')

    await input.setValue('\u{10428}')
    await tick()
    const supplementaryMark = optionFor(wrapper, 'row-supplementary').find('mark')
    expect(supplementaryMark.text()).toBe('\u{10400}')
    expect(Array.from(supplementaryMark.text())).toHaveLength(1)

    await input.setValue('i')
    await tick()
    const dottedIMark = optionFor(wrapper, 'row-idot').find('mark')
    expect(dottedIMark.text()).toBe('İ')
    expect(Array.from(dottedIMark.text())).toHaveLength(1)
    expect(optionFor(wrapper, 'row-metadata').findAll('mark')).toHaveLength(0)
  })

  // spec: session-navigation/local-search-applies-immediately
  it('filters accepted local rows immediately without API, action, configuration, or telemetry calls', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('waiting'), row('target')]) }))
    await tick()

    const methods = [
      client.snapshot as ReturnType<typeof vi.fn>,
      client.action as ReturnType<typeof vi.fn>,
      client.configuration as ReturnType<typeof vi.fn>,
      client.configure as ReturnType<typeof vi.fn>,
      client.release as ReturnType<typeof vi.fn>,
      client.telemetry as ReturnType<typeof vi.fn>,
    ]
    const before = methods.map((method) => method.mock.calls.length)
    const input = await openSearch(wrapper)
    await input.setValue('target')
    await tick()

    expect(wrapper.findAll('[role="option"]').map((option) => option.text())).toHaveLength(1)
    expect(wrapper.findAll('[role="option"]').map((option) => option.attributes('id'))).toEqual([
      expect.stringContaining('skill/target'),
    ])
    expect(methods.map((method) => method.mock.calls.length)).toEqual(before)
  })

  // spec: session-navigation/applied-query-and-count-remain-visible
  // spec: session-navigation/search-resumes-the-current-query
  // spec: session-navigation/clear-and-refresh
  // spec: session-navigation/status-grouped-resource-order-and-filtering
  it('confirms and resumes the query while temporary folds leave saved folds intact on clear', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([
      row('required-readme', { id: 'row-required', group: 'required', required: true, searchWords: ['readme', 'required-readme'] }),
      row('readme-helper', { id: 'row-helper', group: 'enabled' }),
      row('alpha', { id: 'row-alpha', group: 'enabled' }),
      row('zeta', { id: 'row-zeta', group: 'enabled' }),
      row('readme', { id: 'row-available', group: 'available', checked: false }),
    ]) }))
    await tick()

    await groupButton(wrapper, 'Required').trigger('click')
    await tick()
    expect(groupButton(wrapper, 'Required').attributes('aria-expanded')).toBe('false')

    const input = await openSearch(wrapper)
    await input.setValue('readme')
    await tick()
    expect(groupButton(wrapper, 'Required').attributes('aria-expanded')).toBe('true')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(3)

    await groupButton(wrapper, 'Enabled').trigger('click')
    await tick()
    expect(groupButton(wrapper, 'Enabled').attributes('aria-expanded')).toBe('false')

    await input.trigger('keydown', { key: 'Enter' })
    await tick()
    expect(wrapper.find('input[aria-label="Search resources"]').exists()).toBe(false)
    expect(wrapper.find('.workspace__search-status').text()).toContain('readme')
    expect(wrapper.find('.workspace__search-status').text()).toContain('3')

    const resumed = await openSearch(wrapper)
    expect((resumed.element as HTMLInputElement).value).toBe('readme')
    await resumed.setValue('helm')
    await tick()
    await resumed.trigger('keydown', { key: 'Escape' })
    await tick()
    expect(wrapper.find('input[aria-label="Search resources"]').exists()).toBe(false)
    expect(wrapper.find('.workspace__search-status').text()).toContain('readme')
    expect(wrapper.find('.workspace__search-status').text()).toContain('3')

    await wrapper.find('button[aria-label="Clear search"]').trigger('click')
    await tick()
    expect(wrapper.find('.workspace__search-status').exists()).toBe(false)
    expect(groupButton(wrapper, 'Required').attributes('aria-expanded')).toBe('false')
    expect(groupButton(wrapper, 'Enabled').attributes('aria-expanded')).toBe('true')
    expect(wrapper.findAll('ul[aria-label="Resources: Enabled"] [role="option"]').map((option) => option.attributes('id'))).toEqual([
      'row-row-alpha', 'row-row-helper', 'row-row-zeta',
    ])
  })

  // spec: session-startup/selection-follows-its-resource-when-rows-are-inserted
  // spec: session-startup/an-open-detail-follows-its-verified-row
  // spec: session-navigation/name-match-outranks-metadata-match
  it('preserves selected identity and detail on an accepted reload, then selects the best match when query changes', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    const selected = row('readme-helper', { id: 'row-helper', group: 'enabled' })
    const exact = row('readme', { id: 'row-readme', group: 'enabled' })
    const metadata = row('required-meta', { id: 'row-required', group: 'required', required: true, searchWords: ['readme', 'required-meta', 'helm'] })
    const available = row('helm', { id: 'row-helm', group: 'available', checked: false })
    push(event({ sequence: 1, payload: snapshot([selected, exact, metadata, available]) }))
    await tick()

    const input = await openSearch(wrapper)
    await input.setValue('readme')
    await tick()
    expect(optionFor(wrapper, 'row-readme').attributes('aria-selected')).toBe('true')
    await optionFor(wrapper, 'row-helper').trigger('click')
    await tick()
    expect(client.telemetry).toHaveBeenCalledTimes(1)
    await input.trigger('keydown', { key: 'Enter' })
    await tick()

    const earlier = row('0-readme', { id: 'row-earlier', group: 'enabled', searchWords: ['0', 'readme'] })
    push(event({ sequence: 2, generation: 2, payload: snapshot([earlier, selected, exact, metadata, available]) }))
    await tick()
    await tick()
    expect(optionFor(wrapper, 'row-helper').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[aria-label="Detail"]').text()).toContain('readme-helper')
    expect(wrapper.find('.workspace__search-status').text()).toContain('4')

    const resumed = await openSearch(wrapper)
    expect((resumed.element as HTMLInputElement).value).toBe('readme')
    await resumed.setValue('helm')
    await tick()
    expect(optionFor(wrapper, 'row-helm').attributes('aria-selected')).toBe('true')
    expect(wrapper.findAll('.workspace__group-toggle').map((button) => button.attributes('aria-label'))).toEqual([
      'Required', 'Available',
    ])
    expect(client.telemetry).toHaveBeenCalledTimes(1)
  })

  // spec: session-navigation/local-search-applies-immediately
  // spec: session-navigation/remote-marketplace-search-is-debounced
  it('filters locally immediately and aborts the superseded Marketplace request', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([
      row('needle-local', { id: 'local-row', group: 'enabled' }),
      row('needle-cached', { id: 'cached-row', group: 'available', checked: false, scope: 'available', status: 'available', discovery: true }),
    ], { discoveryEnabled: true, discoveryHealth: [{ id: 'market', label: 'Marketplace', total: 1, truncated: false, cacheState: 'fresh', message: '' }] }) }))
    await tick()

    const input = await openSearch(wrapper)
    await input.setValue('need')
    await tick()
    expect(wrapper.findAll('[role="option"]').map((option) => option.attributes('id'))).toEqual(['row-local-row', 'row-cached-row'])

    const discovery = (client as unknown as { discovery: ReturnType<typeof vi.fn> }).discovery
    const firstCalls = discovery.mock.calls as unknown as [string, number, AbortSignal?][]
    if (firstCalls.length !== 1 || firstCalls[0][0] !== 'need' || firstCalls[0][1] !== 1) {
      throw new Error('first current discovery request was not issued')
    }
    const firstSignal = firstCalls[0][2]
    expect(firstSignal?.aborted).toBe(false)

    await input.setValue('needle')
    await tick()
    const calls = discovery.mock.calls as unknown as [string, number, AbortSignal?][]
    if (calls.length !== 2 || calls[1][0] !== 'needle' || calls[1][1] !== 2) {
      throw new Error('superseding discovery request was not issued')
    }
    expect(firstSignal?.aborted).toBe(true)
    expect(calls[1][2]?.aborted).toBe(false)
    expect(wrapper.find('.workspace__search-status').text()).toContain('1 local match')
    expect(client.telemetry).not.toHaveBeenCalled()
  })
})

describe('search toolbar slot', () => {
  it('puts the input in the pill slot while editing and shows the applied query and count in the pill', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('review-a'), row('review-b'), row('other')]) }))
    await tick()

    const input = await openSearch(wrapper)
    expect(wrapper.find('button[aria-label="Search"]').exists()).toBe(false)
    await input.setValue('review')
    await tick()
    await input.trigger('keydown', { key: 'Enter' })
    await tick()

    const pill = wrapper.get('button[aria-label="Search"]')
    expect(pill.text()).toContain('review')
    expect(pill.text()).toContain('· 2')
    expect(pill.text()).not.toContain('⌘K')
    const status = wrapper.get('.workspace__search-status')
    expect(status.text()).toContain('review · 2 local matches')
    expect(status.classes()).toContain('gui-visually-hidden')
  })

  it('explains when the applied search matches no resource', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    push(event({ sequence: 1, payload: snapshot([row('alpha')]) }))
    await tick()

    const input = await openSearch(wrapper)
    await input.setValue('zzqx')
    await tick()
    await input.trigger('keydown', { key: 'Enter' })
    await tick()

    expect(wrapper.get('.gui-empty--muted').text()).toBe('No resources match “zzqx”.')
    expect(wrapper.find('[role="option"]').exists()).toBe(false)
  })
})
