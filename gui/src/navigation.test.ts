// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { event, fakeClient, mountWorkspace, row, snapshot, tick } from './test-support'

function optionFor(wrapper: ReturnType<typeof mountWorkspace>['wrapper'], id: string) {
  const option = wrapper.findAll('[role="option"]').find((entry) => entry.attributes('id') === `row-${id}`)
  if (option == null) {
    throw new Error(`missing resource option ${id}`)
  }
  return option
}

function groupButton(wrapper: ReturnType<typeof mountWorkspace>['wrapper'], id: string) {
  const button = wrapper.findAll('.workspace__group-toggle').find((entry) => entry.attributes('aria-label') === id)
  if (button == null) {
    throw new Error(`missing status group ${id}`)
  }
  return button
}

function rect(top: number): DOMRect {
  return { x: 0, y: top, top, right: 200, bottom: top + 20, left: 0, width: 200, height: 20, toJSON: () => ({}) } as DOMRect
}

describe('status-grouped navigation', () => {
  // spec: session-navigation/status-grouped-resource-order-and-filtering
  it('uses backend group order, stable alphabetical leaves, and omits empty groups', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    const rows = [
      row('malformed', { group: 'malformed', status: 'malformed: invalid' }),
      row('zulu', { group: 'enabled', name: 'zulu', id: 'row-zulu' }),
      row('same-z', { group: 'enabled', name: 'same', id: 'row-z' }),
      row('same-a', { group: 'enabled', name: 'same', id: 'row-a' }),
      row('stale', { group: 'stale', status: 'available, stale' }),
      row('broken', { group: 'broken', status: 'broken' }),
      row('available', { group: 'available', checked: false, status: 'available' }),
      row('pending', { group: 'pending', status: 'enabled (pending sync)' }),
      row('required', { group: 'required', required: true, status: 'required' }),
    ]
    push(event({ sequence: 1, payload: snapshot(rows) }))
    await tick()

    expect(wrapper.findAll('.workspace__group-toggle').map((button) => button.attributes('aria-label'))).toEqual([
      'Required', 'Enabled', 'Pending sync', 'Stale', 'Broken', 'Available', 'Malformed',
    ])
    expect(wrapper.findAll('ul[aria-label="Resources: Enabled"] [role="option"]').map((option) => option.attributes('id'))).toEqual([
      'row-row-a', 'row-row-z', 'row-row-zulu',
    ])

    push(event({ sequence: 2, generation: 2, payload: snapshot([row('only-available', { group: 'available', status: 'available' })]) }))
    await tick()
    expect(wrapper.findAll('.workspace__group-toggle').map((button) => button.attributes('aria-label'))).toEqual(['Available'])
  })

  // spec: session-navigation/status-grouped-resource-order-and-filtering
  it('folds only one group and preserves the folded selected section on reload', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    const initial = [
      row('enabled-a', { group: 'enabled' }),
      row('enabled-b', { group: 'enabled' }),
      row('required', { group: 'required', required: true, status: 'required' }),
      row('available', { group: 'available', checked: false, status: 'available' }),
    ]
    push(event({ sequence: 1, payload: snapshot(initial) }))
    await tick()

    const enabled = groupButton(wrapper, 'Enabled')
    await enabled.trigger('click')
    await tick()
    expect(enabled.attributes('aria-expanded')).toBe('false')
    expect(enabled.attributes('aria-current')).toBe('true')
    expect(wrapper.findAll('ul[aria-label="Resources: Enabled"] [role="option"]')).toHaveLength(0)
    expect(wrapper.findAll('ul[aria-label="Resources: Required"] [role="option"]')).toHaveLength(1)
    expect(wrapper.findAll('ul[aria-label="Resources: Available"] [role="option"]')).toHaveLength(1)

    push(event({
      sequence: 2,
      generation: 2,
      payload: snapshot([
        row('enabled-c', { group: 'enabled' }),
        row('required', { group: 'required', required: true, status: 'required' }),
        row('available', { group: 'available', checked: false, status: 'available' }),
      ]),
    }))
    await tick()

    expect(groupButton(wrapper, 'Enabled').attributes('aria-expanded')).toBe('false')
    expect(groupButton(wrapper, 'Enabled').attributes('aria-current')).toBe('true')
    expect(wrapper.findAll('ul[aria-label="Resources: Enabled"] [role="option"]')).toHaveLength(0)
    expect(wrapper.findAll('ul[aria-label="Resources: Available"] [role="option"]')).toHaveLength(1)
  })

  // spec: session-startup/selection-follows-its-resource-when-rows-are-inserted
  it('keeps a selected row and its viewport position when an earlier row is inserted', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    const a = row('a', { group: 'enabled', id: 'row-a' })
    const selected = row('selected', { group: 'enabled', id: 'row-selected' })
    const z = row('z', { group: 'enabled', id: 'row-z' })
    push(event({ sequence: 1, payload: snapshot([a, selected, z]) }))
    await tick()
    expect(optionFor(wrapper, a.id).attributes('aria-selected')).toBe('true')

    await optionFor(wrapper, selected.id).trigger('click')
    const list = wrapper.find('.workspace__list').element as HTMLElement
    list.scrollTop = 40
    const listBounds = vi.spyOn(list, 'getBoundingClientRect').mockReturnValue(rect(100))
    const selectedElement = optionFor(wrapper, selected.id).element as HTMLElement
    const rowBounds = vi.spyOn(selectedElement, 'getBoundingClientRect')
    rowBounds.mockReturnValueOnce(rect(220)).mockReturnValueOnce(rect(235)).mockReturnValue(rect(220))

    const earlier = row('earlier', { group: 'enabled', id: 'row-earlier', name: '0-earlier' })
    push(event({ sequence: 2, generation: 2, payload: snapshot([earlier, a, selected, z]) }))
    await tick()
    await tick()

    const selectedAfter = optionFor(wrapper, selected.id)
    expect(selectedAfter.attributes('aria-selected')).toBe('true')
    expect(selectedAfter.attributes('id')).toBe('row-row-selected')
    expect(list.scrollTop).toBe(55)
    const relativeTop = selectedAfter.element.getBoundingClientRect().top - list.getBoundingClientRect().top
    expect(relativeTop).toBe(120)
    expect(listBounds).toHaveBeenCalled()
  })

  // spec: session-startup/selection-follows-its-resource-when-rows-are-inserted
  it('keeps the selected viewport when browser scroll anchoring already adjusted scrollTop', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    const a = row('a', { group: 'enabled', id: 'row-a' })
    const selected = row('selected', { group: 'enabled', id: 'row-selected' })
    const z = row('z', { group: 'enabled', id: 'row-z' })
    push(event({ sequence: 1, payload: snapshot([a, selected, z]) }))
    await tick()

    await optionFor(wrapper, selected.id).trigger('click')
    const list = wrapper.find('.workspace__list').element as HTMLElement
    list.scrollTop = 40
    vi.spyOn(list, 'getBoundingClientRect').mockReturnValue(rect(100))
    const selectedElement = optionFor(wrapper, selected.id).element as HTMLElement
    let rowBoundsCalls = 0
    vi.spyOn(selectedElement, 'getBoundingClientRect').mockImplementation(() => {
      rowBoundsCalls++
      if (rowBoundsCalls === 2) {
        list.scrollTop = 55
      }
      return rect(220)
    })

    const earlier = row('earlier', { group: 'enabled', id: 'row-earlier', name: '0-earlier' })
    push(event({ sequence: 2, generation: 2, payload: snapshot([earlier, a, selected, z]) }))
    await tick()
    await tick()

    const selectedAfter = optionFor(wrapper, selected.id)
    expect(selectedAfter.attributes('id')).toBe('row-row-selected')
    expect(selectedAfter.attributes('aria-selected')).toBe('true')
    expect(rowBoundsCalls).toBeGreaterThanOrEqual(2)
    expect(list.scrollTop).toBe(55)
    expect(selectedAfter.element.getBoundingClientRect().top - list.getBoundingClientRect().top).toBe(120)
  })

  // spec: session-navigation/status-grouped-resource-order-and-filtering
  // spec: session-startup/selection-follows-its-resource-when-rows-are-inserted
  it('selects the next surviving row below a removed selection, then the nearest preceding row', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    const a = row('a', { group: 'enabled', id: 'row-a' })
    const b = row('b', { group: 'enabled', id: 'row-b' })
    const c = row('c', { group: 'enabled', id: 'row-c' })
    const d = row('d', { group: 'enabled', id: 'row-d' })
    const earlier = row('earlier', { group: 'enabled', id: 'row-earlier', name: '0-earlier' })
    push(event({ sequence: 1, payload: snapshot([a, b, c, d]) }))
    await tick()
    await optionFor(wrapper, b.id).trigger('click')

    push(event({ sequence: 2, generation: 2, payload: snapshot([earlier, a, c, d]) }))
    await tick()
    expect(optionFor(wrapper, c.id).attributes('aria-selected')).toBe('true')

    push(event({ sequence: 3, generation: 3, payload: snapshot([earlier, a, d]) }))
    await tick()
    expect(optionFor(wrapper, d.id).attributes('aria-selected')).toBe('true')

    push(event({ sequence: 4, generation: 4, payload: snapshot([earlier, a]) }))
    await tick()
    expect(optionFor(wrapper, a.id).attributes('aria-selected')).toBe('true')

    push(event({ sequence: 5, generation: 5, payload: snapshot([]) }))
    await tick()
    expect(wrapper.findAll('[role="option"][aria-selected="true"]')).toHaveLength(0)
    expect(wrapper.find('[aria-label="Detail"]').text()).toContain('Select a resource to inspect it.')
  })

  // spec: session-startup/an-open-detail-follows-its-verified-row
  it('retains detail identity across provisional stages and updates only from final verified metadata', async () => {
    const { client, push } = fakeClient()
    const { wrapper } = mountWorkspace(client)
    const selected = row('selected', { group: 'enabled', id: 'row-selected', description: 'initial description' })
    const other = row('other', { group: 'enabled', id: 'row-other', description: 'other description' })
    push(event({ sequence: 1, generation: 1, payload: snapshot([selected, other]) }))
    await tick()
    await optionFor(wrapper, selected.id).trigger('click')
    expect(wrapper.find('[aria-label="Detail"]').text()).toContain('initial description')

    push(event({
      sequence: 2,
      generation: 2,
      payload: snapshot([row('selected', { group: 'enabled', id: 'row-selected', description: 'provisional description' }), other], { stage: 'provisional', drift: 'pending' }),
    }))
    await tick()
    expect(wrapper.find('[aria-label="Detail"]').text()).toContain('initial description')
    expect(wrapper.find('[aria-label="Detail"]').text()).not.toContain('provisional description')

    push(event({
      sequence: 3,
      generation: 2,
      payload: snapshot([row('selected', { group: 'enabled', id: 'row-selected', description: 'inventory description' }), other], { stage: 'inventory', drift: 'pending' }),
    }))
    await tick()
    expect(wrapper.find('[aria-label="Detail"]').text()).toContain('initial description')

    push(event({
      sequence: 4,
      generation: 2,
      payload: snapshot([row('selected', { group: 'enabled', id: 'row-selected', description: 'verified description' }), other]),
    }))
    await tick()
    expect(optionFor(wrapper, selected.id).attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[aria-label="Detail"]').text()).toContain('verified description')
    expect(wrapper.find('[aria-label="Detail"]').text()).not.toContain('other description')
  })
})
