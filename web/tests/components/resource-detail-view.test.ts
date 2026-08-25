import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ResourceDetailView from '../../app/components/ResourceDetailView.vue'

const stubs = {
  NuxtLink: defineComponent({ props: { to: { type: [String, Object], default: '' } }, setup(props, { attrs, slots }) { return () => h('a', { ...attrs, href: String(props.to) }, slots.default?.()) } }),
  UIcon: defineComponent({ setup: () => () => h('span') }),
  UBadge: defineComponent({ setup(_, { attrs, slots }) { return () => h('span', attrs, slots.default?.()) } }),
  UButton: defineComponent({ setup(_, { attrs, slots }) { return () => h('button', attrs, slots.default?.()) } }),
}

const digest = 'sha256:' + 'c'.repeat(64)

const resource = {
  coordinate: { marketplace: 'marketplace', source: 'team/tools', kind: 'skill', name: 'review' },
  content_digest: digest,
  acquisition: 'mirror',
  license: 'Apache-2.0',
  provenance: { repository: 'https://example.test/repo', commit: 'a'.repeat(40) },
  findings: [{ code: 'metadata-check', severity: 'low', policy: 'content-policy', message: '<script>not markup</script>' }],
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
    expect(wrapper.get('button[aria-label^="Copy pinned browser"]').exists()).toBe(true)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('[onerror]').exists()).toBe(false)
    expect(wrapper.find('[aria-live="polite"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Severity: low')
    expect(wrapper.text()).toContain('Policy: content-policy')
    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.findAll('h2').length).toBeGreaterThan(1)
  })

  it('renders exact digest revision links and preserves revoked history state', () => {
    const previousDigest = 'sha256:' + 'd'.repeat(64)
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource,
        source: 'source', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example',
        revisions: [
          { ...resource, content_digest: previousDigest, revocation: { reason: 'policy' } },
        ],
      },
      global: { stubs },
    })

    expect(wrapper.get('[aria-labelledby="revision-history-heading"]').text()).toContain(previousDigest)
    expect(wrapper.get(`a[href="https://market.example/s/source/skill/review/revisions/${encodeURIComponent(previousDigest)}"]`).exists()).toBe(true)
    expect(wrapper.get('[aria-labelledby="revision-history-heading"]').text()).toContain('Revoked')
    expect(wrapper.get('[aria-labelledby="revision-history-heading"]').text()).toContain('Current')
  })

  it('renders only sanitized Markdown and an escaped raw view from the inspection projection', () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource,
        source: 'source', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example',
        inspection: {
          schema_version: 'v1', coordinate: { marketplace: 'marketplace', source: 'source', kind: 'skill', name: 'review' }, revision_digest: digest, primary_path: 'SKILL.md',
          files: [{ path: 'SKILL.md', content_digest: digest, media_type: 'text/markdown', size: 44, inspection_class: 'text' }],
          findings: [{ code: 'remote-reference', severity: 'warn', policy: 'network-policy', path: 'SKILL.md', start_byte: 1, end_byte: 2, message: '<script>finding</script>' }],
        },
        primaryContent: '# Hello\n\n[docs](https://example.com/docs)\n\n![remote image](https://evil.invalid/pixel.png)\n\n<div>raw HTML</div>\n<script>alert(1)</script>',
      },
      global: { stubs },
    })

    expect(wrapper.get('[data-testid="safe-markdown"]').html()).toContain('<h1>Hello</h1>')
    expect(wrapper.get('[data-testid="safe-markdown"]').html()).not.toContain('<script>')
    expect(wrapper.get('[data-testid="safe-markdown"]').html()).not.toContain('<img')
    expect(wrapper.get('[data-testid="safe-markdown"]').html()).not.toContain('evil.invalid')
    expect(wrapper.get('[data-testid="safe-markdown"]').html()).toContain('href="https://example.com/docs"')
    expect(wrapper.get('[data-testid="escaped-raw"]').html()).toContain('&lt;script&gt;')
    expect(wrapper.get('[data-testid="escaped-raw"] mark').attributes('aria-label')).toContain('remote-reference (warn)')
    expect(wrapper.get('a[href="#finding-highlight-0"]').text()).toBe('View highlighted range')
    expect(wrapper.text()).toContain('remote-reference')
    expect(wrapper.text()).toContain('Severity: warn')
    expect(wrapper.text()).toContain('Policy: network-policy')
    expect(wrapper.text()).toContain('SKILL.md:1-2')
  })

  it('lazily fetches selected text files by exact digest and reports unavailable files', async () => {
    const primaryDigest = digest
    const notesDigest = 'sha256:' + 'd'.repeat(64)
    const configDigest = 'sha256:' + 'e'.repeat(64)
    const unavailableDigest = 'sha256:' + 'f'.repeat(64)
    const calls: Array<{ path: string; digest: string }> = []
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource,
        source: 'source', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example',
        inspection: {
          schema_version: 'v1', coordinate: { marketplace: 'marketplace', source: 'source', kind: 'skill', name: 'review' }, revision_digest: digest, primary_path: 'SKILL.md',
          files: [
            { path: 'SKILL.md', content_digest: primaryDigest, media_type: 'text/markdown', size: 10, inspection_class: 'text' },
            { path: 'support/notes.md', content_digest: notesDigest, media_type: 'text/markdown', size: 20, inspection_class: 'text' },
            { path: 'support/config.json', content_digest: configDigest, media_type: 'application/json', size: 20, inspection_class: 'text' },
            { path: 'support/tool.bin', content_digest: unavailableDigest, media_type: 'application/octet-stream', size: 4, inspection_class: 'metadata' },
            { path: 'support/missing.txt', content_digest: 'sha256:' + 'b'.repeat(64), media_type: 'text/plain', size: 4, inspection_class: 'text' },
          ],
        },
        primaryContent: '# Primary',
        loadInspectionFile: async file => {
          calls.push({ path: file.path, digest: file.content_digest })
          if (file.path.endsWith('notes.md')) return '# Notes\n\n<script>removed</script>'
          if (file.path.endsWith('config.json')) return '{"value":"<script>escaped</script>"}'
          throw new Error('unavailable')
        },
      },
      global: { stubs },
    })

    const selector = wrapper.get('select[aria-label="Inspection file"]')
    expect(selector.element.value).toBe(primaryDigest)
    expect(calls).toEqual([])

    await selector.setValue(notesDigest)
    await flushPromises()
    expect(calls).toEqual([{ path: 'support/notes.md', digest: notesDigest }])
    expect(wrapper.get('[data-testid="safe-markdown"]').text()).toContain('Notes')
    expect(wrapper.get('[data-testid="safe-markdown"]').html()).not.toContain('<script>')

    await selector.setValue(configDigest)
    await flushPromises()
    expect(calls.at(-1)).toEqual({ path: 'support/config.json', digest: configDigest })
    expect(wrapper.find('[data-testid="safe-markdown"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="escaped-selected-raw"]').html()).toContain('&lt;script&gt;')
    expect(wrapper.find('script').exists()).toBe(false)

    await selector.setValue('sha256:' + 'b'.repeat(64))
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('unavailable')
    expect(wrapper.find('script').exists()).toBe(false)
  })

  it('shows all-kind metadata without rendering SVG or binary bodies', () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, coordinate: { ...resource.coordinate, kind: 'command', name: 'build' } },
        source: 'source', kind: 'command', name: 'build', canonicalUrl: 'https://market.example', apiBase: '/',
        inspection: {
          schema_version: 'v1', coordinate: { marketplace: 'marketplace', source: 'source', kind: 'command', name: 'build' }, revision_digest: digest, primary_path: 'COMMAND.md',
          files: [
            { path: 'COMMAND.md', content_digest: digest, media_type: 'text/plain', size: 4, inspection_class: 'text' },
            { path: 'support/diagram.svg', content_digest: 'sha256:' + 'd'.repeat(64), media_type: 'application/octet-stream', size: 12, inspection_class: 'metadata' },
          ],
        },
        primaryContent: 'make',
      },
      global: { stubs },
    })
    expect(wrapper.text()).toContain('COMMAND.md')
    expect(wrapper.text()).toContain('Binary metadata (not rendered)')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('svg').exists()).toBe(false)
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
    expect(wrapper.get('button[aria-label^="Copy pinned CLI"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button[aria-label^="Copy pinned browser"]').exists()).toBe(true)
    expect(wrapper.get('button[aria-label^="Copy pinned browser"]').attributes('disabled')).toBeUndefined()
  })
})
