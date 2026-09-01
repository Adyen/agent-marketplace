import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import RegistryHistoryList from '../../app/components/RegistryHistoryList.vue'
import RegistryOverview from '../../app/components/RegistryOverview.vue'

const stubs = {
  UBadge: defineComponent({ setup(_, { slots }) { return () => h('span', slots.default?.()) } }),
}

const hostile = '<script>alert("x")</script>'
const registry = {
  id: hostile,
  slug: hostile,
  aliases: [hostile],
  origins: [hostile],
  ref: hostile,
  roots: [{ path: hostile, source_harness: hostile }],
  acquisition: 'mirror' as const,
  publisher: { name: hostile, url: hostile },
  license: hostile,
  listed: true,
}

const occurrence = {
  registry_id: hostile,
  ref: hostile,
  commit: hostile,
  registry_revision_manifest_digest: hostile,
  acquisition: 'mirror' as const,
  mirrored_artifact_digest: hostile,
  occurred_at: '2026-01-02T03:04:05Z',
}

describe('registry discovery surfaces', () => {
  it('renders registry metadata and hostile text as escaped text, with publisher/operator distinction', () => {
    const wrapper = mount(RegistryOverview, {
      props: { registry, operator: { id: 'operator-id', name: 'Marketplace operator', canonical_url: 'https://market.example' } },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('Registry publisher')
    expect(wrapper.text()).toContain('Marketplace operator')
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.text()).toContain(hostile)
  })

  it('provides an accessible empty history state', () => {
    const wrapper = mount(RegistryHistoryList, { props: { occurrences: [] }, global: { stubs } })
    expect(wrapper.text()).toContain('No registry occurrences have been recorded.')
    expect(wrapper.find('[aria-label="Registry occurrence timeline"]').exists()).toBe(false)
  })

  it('renders exact history fields as text without interpreting hostile values', () => {
    const wrapper = mount(RegistryHistoryList, { props: { occurrences: [occurrence] }, global: { stubs } })
    expect(wrapper.get('ol[aria-label="Registry occurrence timeline"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Commit')
    expect(wrapper.text()).toContain('Ref')
    expect(wrapper.text()).toContain('Registry revision manifest digest')
    expect(wrapper.text()).toContain(hostile)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('time').attributes('datetime')).toBe(occurrence.occurred_at)
  })
})
