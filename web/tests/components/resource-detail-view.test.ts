import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ResourceDetailView from '../../app/components/ResourceDetailView.vue'

const stubs = {
  UIcon: defineComponent({ setup: () => () => h('span') }),
  UBadge: defineComponent({ setup(_, { slots }) { return () => h('span', slots.default?.()) } }),
  UButton: defineComponent({ setup(_, { attrs, slots }) { return () => h('button', attrs, slots.default?.()) } }),
}

const digest = 'sha256:' + 'c'.repeat(64)

const resource = {
  coordinate: { source: 'team/tools', kind: 'skill', name: 'review' },
  content_digest: digest,
  acquisition: 'mirror',
  license: 'Apache-2.0',
  provenance: { repository: 'https://example.test/repo', commit: 'abc123' },
  findings: [{ severity: 'low', message: '<script>not markup</script>' }],
}

describe('ResourceDetailView', () => {
  it('shows read-only provenance, findings, and digest-pinned actions as text', () => {
    const wrapper = mount(ResourceDetailView, {
      props: { resource, source: 'team-tools', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example' },
      global: { stubs },
    })

    expect(wrapper.get('h1').text()).toBe('review')
    expect(wrapper.text()).toContain('mirror')
    expect(wrapper.text()).toContain('Apache-2.0')
    expect(wrapper.text()).toContain('<script>not markup</script>')
    expect(wrapper.get('code').text()).toContain(`/revisions/sha256%3A${'c'.repeat(64)}`)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('[onerror]').exists()).toBe(false)
    expect(wrapper.find('[aria-live="polite"]').exists()).toBe(true)
    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.findAll('h2').length).toBeGreaterThan(1)
  })

  it('announces revoked content and keeps copy controls keyboard accessible', () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, revocation: { reason: 'security' } },
        source: 'source', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example',
      },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('This revision is revoked')
    expect(wrapper.get('[role="alert"]').text()).toContain('This revision is revoked')
    expect(wrapper.get('button[aria-label^="Copy pinned CLI"]').exists()).toBe(true)
    expect(wrapper.get('button[aria-label^="Copy pinned browser"]').exists()).toBe(true)
  })
})
