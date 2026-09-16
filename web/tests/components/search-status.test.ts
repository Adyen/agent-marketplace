import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SearchStatus from '../../app/components/SearchStatus.vue'

const stubs = {
  UAlert: defineComponent({
    props: { title: String, description: String },
    setup(props, { slots, attrs }) { return () => h('div', { ...attrs }, [h('strong', props.title), h('p', props.description), slots.default?.(), slots.actions?.()]) },
  }),
  UButton: defineComponent({ emits: ['click'], setup(_, { emit, slots }) { return () => h('button', { onClick: () => emit('click') }, slots.default?.()) } }),
  UIcon: defineComponent({ setup: () => () => h('span') }),
}

describe('SearchStatus', () => {
  it('renders an accessible retry action for errors', async () => {
    const wrapper = mount(SearchStatus, { props: { state: 'error' }, global: { stubs } })
    expect(wrapper.get('[role="alert"]').text()).toContain('Search could not be completed')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('renders a reset action for empty results', async () => {
    const wrapper = mount(SearchStatus, { props: { state: 'empty' }, global: { stubs } })
    expect(wrapper.text()).toContain('No resources found')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('reset')).toHaveLength(1)
  })
})
