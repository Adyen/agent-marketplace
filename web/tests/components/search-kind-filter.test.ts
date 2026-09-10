import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SearchKindFilter from '../../app/components/SearchKindFilter.vue'
import { ALL_RESOURCE_KINDS } from '../../app/types/marketplace'

const USelect = defineComponent({
  name: 'USelect',
  props: { modelValue: String, items: Array },
  emits: ['update:modelValue'],
  setup: (_, { attrs }) => () => h('button', attrs),
})

describe('SearchKindFilter', () => {
  it('keeps empty values out of USelect and translates the all-kinds sentinel', async () => {
    const wrapper = mount(SearchKindFilter, {
      props: { modelValue: '', kinds: ['skill', 'command'] },
      global: { stubs: { USelect } },
    })
    const select = wrapper.getComponent(USelect)

    expect(select.props('modelValue')).toBe(ALL_RESOURCE_KINDS)
    expect(select.props('items')).toEqual([
      { label: 'All resource kinds', value: ALL_RESOURCE_KINDS },
      { label: 'Skill', value: 'skill' },
      { label: 'Command', value: 'command' },
    ])
    expect(select.props('items')).not.toContainEqual(expect.objectContaining({ value: '' }))

    select.vm.$emit('update:modelValue', 'command')
    await nextTick()
    select.vm.$emit('update:modelValue', ALL_RESOURCE_KINDS)
    await nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([['command'], ['']])
  })
})
