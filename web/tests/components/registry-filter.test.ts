import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import RegistryFilter from '../../app/components/RegistryFilter.vue'

const USelect = defineComponent({
  name: 'USelect',
  props: { modelValue: Array, items: Array, placeholder: String },
  emits: ['update:modelValue'],
  setup: (props, { attrs, slots }) => () => h('button', attrs, slots.default?.({
    modelValue: props.modelValue,
    open: false,
    ui: {},
  })),
})

describe('RegistryFilter', () => {
  it('shows a placeholder when empty and one truncated selection plus the remaining count', async () => {
    const wrapper = mount(RegistryFilter, {
      props: {
        modelValue: [],
        items: ['registry-with-an-extremely-long-name', 'community-tools', 'team-tools'],
      },
      global: { stubs: { USelect } },
    })

    expect(wrapper.text()).toBe('Any registry')
    expect(wrapper.getComponent(USelect).props('placeholder')).toBe('Any registry')

    await wrapper.setProps({ modelValue: ['registry-with-an-extremely-long-name', 'community-tools', 'team-tools'] })

    const firstSelection = wrapper.get('[data-testid="registry-filter-first"]')
    expect(firstSelection.text()).toBe('registry-with-an-extremely-long-name')
    expect(firstSelection.attributes('title')).toBe('registry-with-an-extremely-long-name')
    expect(firstSelection.classes()).toContain('truncate')
    expect(wrapper.get('[data-testid="registry-filter-more"]').text()).toBe('+2 more')

    wrapper.getComponent(USelect).vm.$emit('update:modelValue', ['team-tools'])
    await nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['team-tools']]])
  })
})
