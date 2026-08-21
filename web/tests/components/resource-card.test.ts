import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ResourceCard from '../../app/components/ResourceCard.vue'

const stubs = {
  NuxtLink: defineComponent({ props: { to: String }, setup(props, { slots }) { return () => h('a', { href: props.to }, slots.default?.()) } }),
  UIcon: defineComponent({ setup: () => () => h('span') }),
  UBadge: defineComponent({ setup(_, { slots }) { return () => h('span', slots.default?.()) } }),
  UButton: defineComponent({ setup(_, { attrs, slots }) { return () => h('button', attrs, slots.default?.()) } }),
}

const hostile = '<img src=x onerror=alert(1)>'

describe('ResourceCard', () => {
  it('escapes remote text and exposes keyboard-complete copy labels', () => {
    const wrapper = mount(ResourceCard, {
      props: {
        canonicalUrl: 'https://market.example',
        item: {
          source_id: hostile,
          source_slug: hostile,
          kind: 'skill',
          name: hostile,
          revision: 'main',
          content_digest: 'sha256:test',
          description: hostile,
        },
      },
      global: { stubs },
    })

    expect(wrapper.get('h2').text()).toBe(hostile)
    expect(wrapper.get('h2').html()).toContain('&lt;img')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('[onerror]').exists()).toBe(false)
    expect(wrapper.get('button[aria-label^="Copy canonical browser URL"]').exists()).toBe(true)
    expect(wrapper.get('button[aria-label^="Copy CLI install command"]').exists()).toBe(true)
  })
})
