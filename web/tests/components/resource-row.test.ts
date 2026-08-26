import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ResourceRow from '../../app/components/ResourceRow.vue'
import ResourceFingerprint from '../../app/components/ResourceFingerprint.vue'

const stubs = {
  NuxtLink: defineComponent({ props: { to: String }, setup(props, { slots }) { return () => h('a', { href: props.to }, slots.default?.()) } }),
  UIcon: defineComponent({ setup: () => () => h('span') }),
  UBadge: defineComponent({ setup(_, { slots }) { return () => h('span', slots.default?.()) } }),
  UButton: defineComponent({ setup(_, { attrs, slots }) { return () => h('button', attrs, slots.default?.()) } }),
  // Rendered for real: the fingerprint is derived from untrusted digest input.
  ResourceFingerprint,
}

const hostile = '<img src=x onerror=alert(1)>'

describe('ResourceRow', () => {
  it('escapes remote text and disables both copy actions for an unroutable item', () => {
    const wrapper = mount(ResourceRow, {
      props: {
        canonicalUrl: 'https://market.example',
        item: {
          source_id: hostile,
          source_slug: hostile,
          kind: 'skill',
          name: hostile,
          revision: 'sha256:' + 'a'.repeat(64),
          content_digest: 'sha256:' + 'b'.repeat(64),
          description: hostile,
        },
      },
      global: { stubs },
    })

    // The coordinate renders as source/name, so the heading holds both.
    expect(wrapper.get('h2').text()).toContain(hostile)
    expect(wrapper.get('h2').html()).toContain('&lt;img')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('[onerror]').exists()).toBe(false)
    expect(wrapper.get('button[aria-label^="Copy canonical browser URL"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button[aria-label^="Install command unavailable"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('a').exists()).toBe(false)
  })

  it('prints the revision digest once, shortened, with the full value on hover', () => {
    const contentDigest = 'sha256:0e90224a1b2c' + '3'.repeat(52)
    const wrapper = mount(ResourceRow, {
      props: {
        canonicalUrl: 'https://market.example',
        item: { source_id: 'source-id', kind: 'skill', name: 'review', revision: contentDigest, content_digest: contentDigest },
      },
      global: { stubs },
    })

    const digest = wrapper.get('span[title]')
    expect(digest.text()).toBe('sha256:0e90224a1b2c…')
    expect(digest.attributes('title')).toBe(contentDigest)
    expect(wrapper.text()).not.toContain(contentDigest)
  })

  it('renders a fingerprint derived from the content digest', () => {
    const wrapper = mount(ResourceRow, {
      props: {
        canonicalUrl: 'https://market.example',
        item: { source_id: 'source-id', kind: 'skill', name: 'review', revision: 'sha256:' + 'd'.repeat(64), content_digest: 'sha256:' + 'd'.repeat(64) },
      },
      global: { stubs },
    })

    const svg = wrapper.get('svg[role="img"]')
    expect(svg.findAll('rect')).toHaveLength(9)
    expect(svg.attributes('aria-label')).toContain('Content fingerprint')
  })

  const digest = 'sha256:' + 'c'.repeat(64)
  const listed = (availability?: string) => ({
    canonicalUrl: 'https://market.example',
    item: { source_id: 'source-id', kind: 'skill' as const, name: 'review', revision: digest, content_digest: digest, ...(availability ? { availability } : {}) },
  })

  it('offers the install command when the instance reports the resource as available', () => {
    const wrapper = mount(ResourceRow, { props: listed('available'), global: { stubs } })

    expect(wrapper.text()).toContain('Available')
    expect(wrapper.get('button[aria-label^="Copy pinned install command"]').attributes('disabled')).toBeUndefined()
  })

  it('withholds the install command when the instance reports the resource as blocked', () => {
    const wrapper = mount(ResourceRow, { props: listed('blocked'), global: { stubs } })

    expect(wrapper.text()).toContain('Not installable')
    // The row states the verdict; the reason rides on the disabled control so
    // the page does not repeat a sentence on every entry.
    const command = wrapper.get('button[aria-label*="cannot be installed"]')
    expect(command.attributes('disabled')).toBeDefined()
    expect(command.attributes('aria-label')).toContain('Not installable')
    // The browser URL stays usable: the revision is still inspectable.
    expect(wrapper.get('button[aria-label^="Copy canonical browser URL"]').attributes('disabled')).toBeUndefined()
  })

  it('asserts no status and gates nothing when the instance reports no availability', () => {
    const wrapper = mount(ResourceRow, { props: listed(), global: { stubs } })

    expect(wrapper.text()).not.toContain('Available')
    expect(wrapper.text()).not.toContain('Not installable')
    expect(wrapper.get('button[aria-label^="Copy pinned install command"]').attributes('disabled')).toBeUndefined()
  })
})
