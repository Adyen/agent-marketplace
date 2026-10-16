import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ResourceRow from '../../app/components/ResourceRow.vue'
import ResourceFingerprint from '../../app/components/ResourceFingerprint.vue'

const stubs = {
  NuxtLink: defineComponent({ props: { to: String }, setup(props, { attrs, slots }) { return () => h('a', { ...attrs, href: props.to }, slots.default?.()) } }),
  ResourceFingerprint,
  UIcon: defineComponent({ setup() { return () => h('span', { 'data-testid': 'info-icon' }) } }),
  UTooltip: defineComponent({
    props: { text: String },
    setup(props, { slots }) {
      return () => h('div', [slots.default?.(), h('span', { role: 'tooltip' }, slots.content?.() ?? props.text)])
    },
  }),
}

const hostile = '<img src=x onerror=alert(1)>'

describe('ResourceRow', () => {
  // spec: marketplace-browser/resource-row-highlights-its-meaningful-coordinate-separator
  it('escapes remote text and disables the install action for an unroutable item', () => {
    const wrapper = mount(ResourceRow, {
      props: {
        canonicalUrl: 'https://market.example',
        item: {
          registry_id: hostile,
          registry_slug: hostile,
          kind: 'skill',
          name: hostile,
          revision: 'sha256:' + 'a'.repeat(64),
          content_digest: 'sha256:' + 'b'.repeat(64),
          description: hostile,
        },
      },
      global: { stubs },
    })

    expect(wrapper.get('h2').text()).toBe(`${hostile}/${hostile}`)
    expect(wrapper.get('.resource-coordinate__separator').text()).toBe('/')
    expect(wrapper.get('.resource-coordinate__separator').classes()).toContain('am-brand-slash')
    expect(wrapper.get('h2').html()).toContain('&lt;img')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('[onerror]').exists()).toBe(false)
    expect(wrapper.get('button[aria-label^="Install command unavailable"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('a').exists()).toBe(false)
    expect(wrapper.get('article').attributes('data-interactive')).toBeUndefined()
    expect(wrapper.text()).not.toContain('View details')
    expect(wrapper.text()).not.toContain('Copy URL')
  })

  it('shows the visual fingerprint and uses one combined resource route', () => {
    const contentDigest = 'sha256:0e90224a1b2c' + '3'.repeat(52)
    const wrapper = mount(ResourceRow, {
      props: {
        canonicalUrl: 'https://market.example',
        item: { registry_id: 'registry-id', kind: 'skill', name: 'review', revision: contentDigest, content_digest: contentDigest },
      },
      global: { stubs },
    })

    expect(wrapper.text()).not.toContain(contentDigest)
    expect(wrapper.find('a[href="/registries/registry-id"]').exists()).toBe(false)
    const coordinateLink = wrapper.get('a[href^="/r/registry-id/skill/review/revisions/"]')
    expect(coordinateLink.text()).toBe('registry-id/review')
    expect(coordinateLink.classes()).toContain('resource-coordinate-link')
  })

  const digest = 'sha256:' + 'c'.repeat(64)
  const listed = (availability?: 'available' | 'blocked' | 'policy-blocked' | 'no-artifact' | 'revoked') => ({
    canonicalUrl: 'https://market.example',
    item: { registry_id: 'registry-id', kind: 'skill' as const, name: 'review', revision: digest, content_digest: digest, ...(availability ? { availability } : {}) },
  })

  it('asserts no status and gates nothing when the instance reports no availability', () => {
    const wrapper = mount(ResourceRow, { props: listed(), global: { stubs } })

    expect(wrapper.text()).not.toContain('Available')
    expect(wrapper.text()).not.toContain('Not installable')
    expect(wrapper.get('article').attributes('data-interactive')).toBe('true')
    expect(wrapper.get('button[aria-label^="Copy install command"]').attributes('disabled')).toBeUndefined()
  })

  // spec: marketplace-browser/browser-displays-counts-while-preserving-filtered-pagination
  it('renders reported installation counts including zero with their meaning', () => {
    const positive = mount(ResourceRow, { props: { ...listed(), item: { ...listed().item, installation_count: 12 } }, global: { stubs } })
    expect(positive.text()).toContain('12 reported installations')
    const explanation = positive.find('[title]')
    expect(explanation.attributes('title')).toMatch(/latest configuration.*30 days/i)
    expect(explanation.attributes('aria-label')).toMatch(/12 reported installations.*latest configuration.*30 days/i)

    const one = mount(ResourceRow, { props: { ...listed(), item: { ...listed().item, installation_count: 1 } }, global: { stubs } })
    expect(one.text()).toContain('1 reported installation')
    expect(one.text()).not.toContain('1 reported installations')

    const zero = mount(ResourceRow, { props: { ...listed(), item: { ...listed().item, installation_count: 0 } }, global: { stubs } })
    expect(zero.text()).toContain('0 reported installations')

    const absent = mount(ResourceRow, { props: listed(), global: { stubs } })
    expect(absent.text()).not.toContain('reported installation')
  })
})
