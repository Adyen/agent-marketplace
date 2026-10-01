import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import CatalogFilterRail from '../../app/components/CatalogFilterRail.vue'

const registries = [
  { id: 'one', slug: 'one' },
  { id: 'two', slug: 'two' },
  { id: 'three', slug: 'three' },
  { id: 'four', slug: 'four' },
  { id: 'five', slug: 'five' },
  { id: 'six', slug: 'six' },
  { id: 'seven', slug: 'seven' },
  { id: 'eight', slug: 'eight' },
]

describe('CatalogFilterRail', () => {
  it('emits kind and registry selections from native controls', async () => {
    const wrapper = mount(CatalogFilterRail, {
      props: {
        kind: '',
        registry: [],
        registries,
        kinds: ['skill', 'command'],
      },
    })

    expect(wrapper.get('input[type="radio"][value=""]').element).toHaveProperty('checked', true)
    expect(wrapper.text()).toContain('All kinds')
    expect(wrapper.text()).toContain('Skill')
    expect(wrapper.text()).toContain('Command')

    await wrapper.get('input[type="radio"][value="command"]').trigger('change')
    expect(wrapper.emitted('update:kind')).toEqual([['command']])

    await wrapper.get('input[type="checkbox"][value="one"]').setValue(true)
    expect(wrapper.emitted('update:registry')).toEqual([[['one']]])
  })

  // spec: marketplace-browser/expansion-action-follows-five-initial-registries
  it('keeps selected registries visible and expands long registry lists', async () => {
    const wrapper = mount(CatalogFilterRail, {
      props: {
        kind: '',
        registry: ['eight'],
        registries,
      },
    })

    expect(wrapper.get('input[type="checkbox"][value="eight"]').element).toHaveProperty('checked', true)
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(6)
    expect(wrapper.attributes('data-expanded')).toBe('false')

    const showAll = wrapper.get('button')
    expect(showAll.text()).toBe('Show all (8)')
    await showAll.trigger('click')
    expect(showAll.text()).toBe('Show fewer')
    expect(wrapper.attributes('data-expanded')).toBe('true')
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(8)
  })
})
