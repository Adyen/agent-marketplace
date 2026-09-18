import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ResourceAuditDetailView from '../../app/components/ResourceAuditDetailView.vue'

const stubs = {
  NuxtLink: defineComponent({ props: { to: { type: [String, Object], default: '' } }, setup(props, { attrs, slots }) { return () => h('a', { ...attrs, href: String(props.to) }, slots.default?.()) } }),
  UBadge: defineComponent({ setup(_, { attrs, slots }) { return () => h('span', attrs, slots.default?.()) } }),
  UButton: defineComponent({ setup(_, { attrs, slots }) { return () => h('button', attrs, slots.default?.()) } }),
}

const digest = 'sha256:' + 'a'.repeat(64)

const audit = {
  analyzer_id: 'builtin.policy',
  analyzer_version: '1.0.0',
  role: 'gating',
  verdict: 'warn',
  summary: 'Network reference needs review.',
  findings: [{
    code: 'remote-reference',
    severity: 'warn',
    policy: 'network-policy',
    message: '<script>audit markup</script>',
    analyzer_id: 'builtin.policy',
    consentable: false,
    location: { path: 'SKILL.md', line: 1, column: 2 },
  }, {
    code: 'legacy-finding',
    severity: 'warn',
    message: 'Omitted consentable is ordinary.',
  }],
  finding_count: 2,
  duration_ns: 3_500_000,
  assessed_at: '2026-01-02T03:04:05Z',
  result_digest: digest,
  error: '',
}

describe('ResourceAuditDetailView', () => {
  it('shows immutable analyzer metadata, verdict badge, and formatted duration', () => {
    const wrapper = mount(ResourceAuditDetailView, {
      props: { audit, registry: 'registry', kind: 'skill', name: 'review', revision: digest },
      global: { stubs },
    })

    expect(wrapper.get('h1').text()).toContain('builtin.policy')
    expect(wrapper.text()).toContain('1.0.0')
    expect(wrapper.text()).toContain('gating')
    expect(wrapper.text()).toContain('warn')
    expect(wrapper.text()).toContain('Network reference needs review.')
    expect(wrapper.text()).toContain('3.50 ms')
    expect(wrapper.text()).toContain('2 findings')
    expect(wrapper.text()).toContain(digest)
    expect(wrapper.text()).toContain('2026-01-02T03:04:05Z')
  })

  it('renders findings attributed to the analyzer and a cannot-be-bypassed label', () => {
    const wrapper = mount(ResourceAuditDetailView, {
      props: { audit, registry: 'registry', kind: 'skill', name: 'review', revision: digest },
      global: { stubs },
    })

    const list = wrapper.get('[aria-label="Analyzer audit findings"]')
    expect(list.findAll('li')).toHaveLength(2)
    expect(list.text()).toContain('remote-reference')
    expect(list.text()).toContain('Severity: warn')
    expect(list.text()).toContain('Policy: network-policy')
    expect(list.text()).toContain('<script>audit markup</script>')
    expect(list.findAll('li')[0].text()).toContain('Cannot be bypassed')
    expect(list.findAll('li')[1].text()).toContain('legacy-finding')
    expect(list.findAll('li')[1].text()).not.toContain('Cannot be bypassed')
  })

  it('links finding locations back to the source revision with a source range query', () => {
    const wrapper = mount(ResourceAuditDetailView, {
      props: { audit, registry: 'registry', kind: 'skill', name: 'review', revision: digest },
      global: { stubs },
    })

    const link = wrapper.get('a[href*="path=SKILL.md"]').attributes('href')
    expect(link).toContain('/r/registry/skill/review/revisions/')
    expect(link).toContain('path=SKILL.md')
    expect(link).toContain('line=1')
    expect(link).toContain('column=2')
  })

  it('escapes HTML-like finding messages instead of rendering them', () => {
    const wrapper = mount(ResourceAuditDetailView, {
      props: { audit, registry: 'registry', kind: 'skill', name: 'review', revision: digest },
      global: { stubs },
    })

    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.html()).toContain('&lt;script&gt;')
  })
})
