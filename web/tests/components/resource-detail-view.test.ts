import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ResourceDetailView from '../../app/components/ResourceDetailView.vue'

import ResourceFingerprint from '../../app/components/ResourceFingerprint.vue'
import CommandBlock from '../../app/components/CommandBlock.vue'

const stubs = {
  // Rendered for real: it is derived from untrusted digest input.
  ResourceFingerprint,
  // Rendered for real: it decides whether the command is shown at all.
  CommandBlock,
  NuxtLink: defineComponent({ props: { to: { type: [String, Object], default: '' } }, setup(props, { attrs, slots }) { return () => h('a', { ...attrs, href: String(props.to) }, slots.default?.()) } }),
  UIcon: defineComponent({ setup: () => () => h('span') }),
  UBadge: defineComponent({ setup(_, { attrs, slots }) { return () => h('span', attrs, slots.default?.()) } }),
  UButton: defineComponent({ setup(_, { attrs, slots }) { return () => h('button', attrs, slots.default?.()) } }),
}

const digest = 'sha256:' + 'c'.repeat(64)

const coordinate = { marketplace: 'marketplace', registry: 'team/tools', kind: 'skill' as const, name: 'review' }
const assessmentBase = {
  coordinate,
  content_digest: digest,
  assessment_digest: 'sha256:' + '1'.repeat(64),
  analyzer_version: 'analyzer-v2',
  policy_digest: 'sha256:' + '2'.repeat(64),
  decision: 'allow' as const,
  inspection_manifest_digest: 'sha256:' + '3'.repeat(64),
  assessed_at: '2026-01-02T03:04:05Z',
}
const occurrence = { coordinate, content_digest: digest, commit: 'a'.repeat(40), manifest_digest: 'sha256:' + '4'.repeat(64) }
const revocation = { kind: 'revision' as const, target_id: digest, reason: 'security', applied_at: '2026-01-03T03:04:05Z' }
const audits = [{
  analyzer_id: 'builtin.policy',
  analyzer_version: '1.0.0',
  role: 'gating',
  verdict: 'pass',
  summary: 'No policy violations.',
  finding_count: 0,
  duration_ns: 1_250_000,
  result_digest: 'sha256:' + '9'.repeat(64),
}]

const resource = {
  coordinate,
  content_digest: digest,
  archive_digest: 'sha256:' + 'a'.repeat(64),
  license: 'Apache-2.0',
  assessment: { ...assessmentBase, findings: [{ code: 'metadata-check', severity: 'low', policy: 'content-policy', message: '<script>not markup</script>' }] },
  occurrence,
  audits,
}

describe('ResourceDetailView', () => {
  it('shows read-only provenance, findings, and digest-pinned actions as text', () => {
    const wrapper = mount(ResourceDetailView, {
      props: { resource, registry: 'team-tools', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example' },
      global: { stubs },
    })

    // The heading is the resource coordinate, which is both the URL and the
    // CLI argument, so the registry is part of the identifier.
    expect(wrapper.get('h1').text()).toBe('team-tools/review')
    expect(wrapper.text()).toContain('analyzer-v2')
    expect(wrapper.text()).toContain('Apache-2.0')
    expect(wrapper.text()).toContain('<script>not markup</script>')
    expect(wrapper.get('code').text()).toContain(`/revisions/sha256%3A${'c'.repeat(64)}`)
    expect(wrapper.get('button[aria-label^="Copy detail URL"]').exists()).toBe(true)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('[onerror]').exists()).toBe(false)
    expect(wrapper.find('[aria-live="polite"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Severity: low')
    expect(wrapper.text()).toContain('Policy: content-policy')
    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.findAll('h2').length).toBeGreaterThan(1)
  })

  it('lists analyzer audits in server order with verdict, duration, and detail links', () => {
    const wrapper = mount(ResourceDetailView, {
      props: { resource, registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example' },
      global: { stubs },
    })

    const section = wrapper.get('[aria-labelledby="audits-heading"]')
    expect(section.text()).toContain('Analyzer audits')
    expect(section.text()).toContain('builtin.policy')
    expect(section.text()).toContain('pass')
    expect(section.text()).toContain('gating')
    expect(section.text()).toContain('No policy violations.')
    expect(section.text()).toContain('1.25 ms')
    expect(section.text()).toContain('0 findings')
    expect(section.get('a').attributes('href')).toContain('/audit/builtin.policy')
  })

  it('renders a restrained empty state for legacy revisions without audits', () => {
    const wrapper = mount(ResourceDetailView, {
      props: { resource: { ...resource, audits: [] }, registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example' },
      global: { stubs },
    })

    expect(wrapper.get('[aria-labelledby="audits-heading"]').text()).toContain('No analyzer audits were reported for this revision.')
  })

  it('renders exact digest revision links and preserves revoked history state', () => {
    const previousDigest = 'sha256:' + 'd'.repeat(64)
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource,
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example',
        revisions: [
          { ...resource, content_digest: previousDigest, revocation: { ...revocation, reason: 'policy' } },
        ],
      },
      global: { stubs },
    })

    expect(wrapper.get('[aria-labelledby="revision-history-heading"]').text()).toContain(previousDigest)
    expect(wrapper.get(`a[href="/r/registry/skill/review/revisions/${encodeURIComponent(previousDigest)}"]`).exists()).toBe(true)
    expect(wrapper.get('[aria-labelledby="revision-history-heading"]').text()).toContain('Revoked')
    expect(wrapper.get('[aria-labelledby="revision-history-heading"]').text()).toContain('Current')
  })

  it.each([
    ['an index-only skill', 'index-only', 'skill'],
    ['a collection', 'mirror', 'collection'],
  ] as const)('keeps an archive-free revision available for %s', (_, acquisition, kind) => {
    const { archive_digest: _archiveDigest, ...withoutArchive } = resource
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: withoutArchive,
        registry: 'registry', kind, name: 'review', canonicalUrl: 'https://market.example',
        acquisition,
        revisions: [withoutArchive],
      },
      global: { stubs },
    })

    const history = wrapper.get('[aria-labelledby="revision-history-heading"]')
    expect(history.text()).toContain('Available')
    expect(history.text()).not.toContain('No installable artifact')
  })

  it('renders only sanitized Markdown and an escaped raw view from the inspection projection', () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, assessment: { ...assessmentBase, findings: [{ code: 'remote-reference', severity: 'warn', policy: 'network-policy', message: '<script>finding</script>', location: { path: 'SKILL.md', line: 1, column: 2 } }] } },
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example',
        inspection: {
          schema_version: 'v1', coordinate: { marketplace: 'marketplace', registry: 'registry', kind: 'skill', name: 'review' }, revision_digest: digest, primary_path: 'SKILL.md',
          files: [{ path: 'SKILL.md', content_digest: digest, media_type: 'text/markdown', size: 44, inspection_class: 'text' }],
        },
        primaryContent: '# Hello\n\n[docs](https://example.com/docs)\n\n![remote image](https://evil.invalid/pixel.png)\n\n<div>raw HTML</div>\n<script>alert(1)</script>',
      },
      global: { stubs },
    })

    expect(wrapper.get('[data-testid="safe-markdown"]').classes()).toContain('marketplace-markdown')
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
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example',
        inspection: {
          schema_version: 'v1', coordinate: { marketplace: 'marketplace', registry: 'registry', kind: 'skill', name: 'review' }, revision_digest: digest, primary_path: 'SKILL.md',
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
        registry: 'registry', kind: 'command', name: 'build', canonicalUrl: 'https://market.example', apiBase: '/',
        inspection: {
          schema_version: 'v1', coordinate: { marketplace: 'marketplace', registry: 'registry', kind: 'command', name: 'build' }, revision_digest: digest, primary_path: 'COMMAND.md',
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
    // No SVG may come from the payload. The only SVG allowed in the tree is the
    // locally generated fingerprint, which is derived from the digest string
    // and never from file content, so assert that every svg present is that.
    const svgs = wrapper.findAll('svg')
    expect(svgs.length).toBeGreaterThan(0)
    for (const svg of svgs) {
      expect(svg.attributes('aria-label')).toContain('Content fingerprint')
    }
  })

  it('announces revoked content and keeps copy controls keyboard accessible', () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, revocation },
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example',
      },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('This revision is revoked')
    expect(wrapper.get('[role="alert"]').text()).toContain('This revision is revoked')
    expect(wrapper.get('button[aria-label^="Copy pinned CLI"]').exists()).toBe(true)
    expect(wrapper.get('button[aria-label^="Copy pinned CLI"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button[aria-label^="Copy detail URL"]').exists()).toBe(true)
    expect(wrapper.get('button[aria-label^="Copy detail URL"]').attributes('disabled')).toBeUndefined()
  })

  it('calls a policy-rejected revision blocked and withholds the install command', () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, archive_digest: undefined, assessment: { ...assessmentBase, decision: 'reject', findings: [{ code: 'shell-exec', severity: 'reject', policy: 'reject', message: 'executes a shell' }] } },
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example',
      },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('Blocked by policy')
    expect(wrapper.text()).not.toContain('Available')
    expect(wrapper.get('[role="alert"]').text()).toContain('The security policy rejected this revision')
    expect(wrapper.find('code').exists()).toBe(false)
    expect(wrapper.get('[role="status"]').text()).toContain('Installation is disabled because the current assessment rejects this revision')
    expect(wrapper.get('[role="status"]').text()).toContain('Metadata and inspection remain available.')
    expect(wrapper.get('button[aria-label^="Copy pinned CLI"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button[aria-label^="Copy detail URL"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[aria-live="polite"]').text()).toContain('Install status for review: Blocked by policy.')
  })

  it('reports a revision with no stored artifact as not installable without claiming a policy block', () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, archive_digest: undefined },
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example',
      },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('No installable artifact')
    expect(wrapper.text()).not.toContain('Blocked by policy')
    expect(wrapper.find('code').exists()).toBe(false)
    expect(wrapper.get('[role="status"]').text()).toContain('Installation is disabled because this Marketplace stores no artifact for this revision')
    expect(wrapper.get('button[aria-label^="Copy pinned CLI"]').attributes('disabled')).toBeDefined()
  })

  it('gives each history row the status its own revision record supports', () => {
    const rejectedDigest = 'sha256:' + 'd'.repeat(64)
    const strippedDigest = 'sha256:' + 'e'.repeat(64)
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource,
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example',
        revisions: [
          { ...resource, content_digest: rejectedDigest, archive_digest: undefined, assessment: { ...assessmentBase, decision: 'reject', findings: [{ code: 'shell-exec', severity: 'reject', policy: 'reject' }] } },
          { ...resource, content_digest: strippedDigest, archive_digest: undefined, assessment: { ...assessmentBase, findings: [] } },
        ],
      },
      global: { stubs },
    })

    const rows = wrapper.get('[aria-labelledby="revision-history-heading"]').findAll('li')
    expect(rows.map(row => row.text())).toEqual([
      expect.stringContaining('Blocked by policy'),
      expect.stringContaining('No installable artifact'),
      expect.stringContaining('Available'),
    ])
    expect(rows[2]?.text()).toContain('Current')
  })

  it('uses assessment finding locations for highlighting', () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, assessment: { ...assessmentBase, findings: [{ code: 'remote-reference', severity: 'warn', policy: 'network-policy', message: 'references a remote host', location: { path: 'SKILL.md', line: 1, column: 2 } }] } },
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example',
        inspection: {
          schema_version: 'v1', coordinate: { marketplace: 'marketplace', registry: 'registry', kind: 'skill', name: 'review' }, revision_digest: digest, primary_path: 'SKILL.md',
          files: [{ path: 'SKILL.md', content_digest: digest, media_type: 'text/markdown', size: 44, inspection_class: 'text' }],
        },
        primaryContent: '# Hello',
      },
      global: { stubs },
    })

    expect(wrapper.findAll('[aria-label="Security findings"] > li')).toHaveLength(1)
    expect(wrapper.get('[aria-label="Security findings"]').text()).toContain('SKILL.md:1-2')
    expect(wrapper.get('[data-testid="escaped-raw"] mark').attributes('aria-label')).toContain('remote-reference')
  })
})
