import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ResourceDetailView from '../../app/components/ResourceDetailView.vue'

import ResourceFingerprint from '../../app/components/ResourceFingerprint.vue'
import CommandBlock from '../../app/components/CommandBlock.vue'

const stubs = {
  // Rendered for real: it is derived from untrusted digest input.
  ResourceFingerprint,
  RegistryOverview: defineComponent({ setup: () => () => h('section', 'Registry details') }),
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
const source = { repository_url: 'https://github.com/Adyen/agent-skills.git', path: 'skills/review/SKILL.md', commit: 'b'.repeat(40) }
const revocation = { kind: 'revision' as const, target_id: digest, reason: 'security', applied_at: '2026-01-03T03:04:05Z' }
const resource = {
  coordinate,
  content_digest: digest,
  archive_digest: 'sha256:' + 'a'.repeat(64),
  license: 'Apache-2.0',
  assessment: { ...assessmentBase, findings: [{ code: 'metadata-check', severity: 'low', policy: 'content-policy', message: '<script>not markup</script>' }] },
  occurrence,
}

describe('ResourceDetailView', () => {
  // spec: marketplace-web-ui/installable-resource-exposes-compact-actions
  it('prioritizes identity, installation, and default content before tabbed supporting details', async () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource,
        registry: 'team-tools', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
        inspection: {
          schema_version: 'v1', coordinate: { marketplace: 'marketplace', registry: 'team/tools', kind: 'skill', name: 'review' }, revision_digest: digest, primary_path: 'SKILL.md',
          files: [{ path: 'SKILL.md', content_digest: digest, media_type: 'text/markdown', size: 10, inspection_class: 'text' }],
        },
        primaryContent: '# Review\n\nResource instructions.',
        revisions: [resource],
      },
      global: { stubs },
    })

    expect(wrapper.get('h1').text()).toBe('review')
    expect(wrapper.find('[data-testid="revision-summary"]').exists()).toBe(false)
    const revisionRow = wrapper.get('[data-testid="revision-row"]')
    expect(revisionRow.get('[data-testid="revision-label"]').text()).toBe('Revision (latest)')
    expect(revisionRow.get('[data-testid="revision-id"]').text()).toBe('c'.repeat(12))
    expect(revisionRow.find('[data-testid="scanned-group"]').exists()).toBe(false)
    expect(revisionRow.find('[data-testid="source-group"]').exists()).toBe(false)
    expect(revisionRow.get('button[aria-label^="Copy revision"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="installation-panel"]').text()).not.toContain('exact version')
    expect(wrapper.get('#installation-heading').text()).toBe('Install')
    expect(wrapper.get('button[aria-label^="Copy pinned CLI"]').text()).toBe('Copy')
    expect(wrapper.find('button[aria-label^="Copy detail URL"]').exists()).toBe(false)
    expect(wrapper.get('button[aria-label^="Copy revision"]').exists()).toBe(true)
    const setupLink = wrapper.get('a[href="/docs/"]')
    expect(setupLink.text()).toBe('Set up CLI')
    expect(setupLink.attributes('target')).toBe('_blank')
    expect(setupLink.attributes('rel')).toContain('noopener')
    expect(setupLink.attributes('aria-label')).toContain('opens in a new tab')

    const installPosition = wrapper.html().indexOf('data-testid="installation-panel"')
    const tabsPosition = wrapper.html().indexOf('role="tablist"')
    const contentPosition = wrapper.html().indexOf('data-testid="safe-markdown"')
    expect(installPosition).toBeLessThan(tabsPosition)
    expect(tabsPosition).toBeLessThan(contentPosition)

    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs.map(tab => tab.text())).toEqual(['Content', 'Security', 'Revisions', 'Registry', 'Technical details'])
    expect(tabs[0]?.attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="safe-markdown"]').text()).toContain('Resource instructions.')
    expect(wrapper.find('[aria-labelledby="security-tab"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('security finding')

    await tabs[1]!.trigger('click')
    expect(wrapper.get('[aria-labelledby="security-tab"]').text()).toContain('metadata-check')
    expect(wrapper.get('[aria-labelledby="security-tab"]').text()).toContain('Scanned Jan 2, 2026')
    expect(wrapper.find('[data-testid="safe-markdown"]').exists()).toBe(false)
  })

  // spec: marketplace-web-ui/forge-hosted-source-links-to-the-pinned-resource-file
  it('shows a GitHub source as the first header item linked to the pinned file', () => {
    const wrapper = mount(ResourceDetailView, {
      props: { resource: { ...resource, source }, registry: 'team-tools', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example' },
      global: { stubs },
    })

    const revisionRow = wrapper.get('[data-testid="revision-row"]')
    expect(revisionRow.element.firstElementChild?.getAttribute('data-testid')).toBe('source-group')
    expect(revisionRow.get('[data-testid="source-label"]').text()).toBe('Source')
    const link = revisionRow.get('[data-testid="source-link"]')
    expect(link.attributes('href')).toBe(`https://github.com/Adyen/agent-skills/blob/${source.commit}/skills/review/SKILL.md`)
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toBe('noopener noreferrer')
    expect(link.text()).toBe('Adyen/agent-skills')
    expect(link.attributes('aria-label')).toBe('View source of review in Adyen/agent-skills (opens in a new tab)')
    expect(revisionRow.get('[data-testid="source-path"]').text()).toBe('skills/review/SKILL.md')
    expect(revisionRow.get('[data-testid="source-path"]').attributes('title')).toBe('skills/review/SKILL.md')
  })

  // spec: marketplace-web-ui/other-host-source-links-to-the-repository
  it('links a source on another host to its repository and still shows the path', () => {
    const wrapper = mount(ResourceDetailView, {
      props: { resource: { ...resource, source: { ...source, repository_url: 'https://git.example/internal/resources.git' } }, registry: 'team-tools', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example' },
      global: { stubs },
    })

    const revisionRow = wrapper.get('[data-testid="revision-row"]')
    expect(revisionRow.get('[data-testid="source-link"]').attributes('href')).toBe('https://git.example/internal/resources')
    expect(revisionRow.get('[data-testid="source-link"]').text()).toBe('internal/resources')
    expect(revisionRow.get('[data-testid="source-path"]').text()).toBe('skills/review/SKILL.md')
  })

  // spec: marketplace-web-ui/resource-without-source-provenance-omits-the-origin
  it('omits the source item when the revision reports no provenance', () => {
    const wrapper = mount(ResourceDetailView, {
      props: { resource, registry: 'team-tools', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example' },
      global: { stubs },
    })

    expect(wrapper.find('[data-testid="source-group"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="revision-row"]').element.firstElementChild?.getAttribute('data-testid')).toBe('revision-group')
  })

  it('labels an older selected revision and links to the current revision', () => {
    const currentDigest = 'sha256:' + 'd'.repeat(64)
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource,
        currentRevisionDigest: currentDigest,
        registry: 'team-tools', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
        revisions: [{ ...resource, content_digest: currentDigest }],
      },
      global: { stubs },
    })

    expect(wrapper.find('[data-testid="revision-summary"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Newer revision available')
    expect(wrapper.get(`a[href="/r/team-tools/skill/review/revisions/${encodeURIComponent(currentDigest)}"]`).text()).toBe('Newer revision available')
  })

  it('shows read-only provenance, findings, and digest-pinned actions as text', async () => {
    const wrapper = mount(ResourceDetailView, {
      props: { resource, registry: 'team-tools', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example' },
      global: { stubs },
    })

    expect(wrapper.get('h1').text()).toBe('review')
    expect(wrapper.get('code').text()).toContain(`/revisions/sha256%3A${'c'.repeat(64)}`)
    expect(wrapper.find('button[aria-label^="Copy detail URL"]').exists()).toBe(false)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('[onerror]').exists()).toBe(false)
    expect(wrapper.find('[aria-live="polite"]').exists()).toBe(true)
    expect(wrapper.findAll('h1')).toHaveLength(1)

    await wrapper.findAll('[role="tab"]')[1]!.trigger('click')
    expect(wrapper.text()).toContain('analyzer-v2')
    expect(wrapper.text()).toContain('<script>not markup</script>')
    expect(wrapper.text()).toContain('Severity: low')
    expect(wrapper.text()).toContain('Policy: content-policy')

    await wrapper.findAll('[role="tab"]')[4]!.trigger('click')
    expect(wrapper.text()).toContain('Apache-2.0')
    expect(wrapper.find('button[aria-label="Copy License"]').exists()).toBe(false)
    expect(wrapper.find('button[aria-label="Copy Archive digest"]').exists()).toBe(true)
    expect(wrapper.findAll('h2').length).toBeGreaterThan(0)
  })

  it('describes automated policy results without presenting them as an endorsement', async () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, assessment: { ...assessmentBase, findings: [] } },
        registry: 'team-tools', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
      },
      global: { stubs },
    })

    expect(wrapper.text()).not.toContain('No findings reported')
    await wrapper.findAll('[role="tab"]')[1]!.trigger('click')
    expect(wrapper.text()).toContain('Allowed by marketplace policy')
    expect(wrapper.text()).toContain('Automated scan: no issues detected')
    expect(wrapper.text()).toContain('Scanned Jan 2, 2026')
  })

  it('keeps the install command without rendering a detail URL action', () => {
    const wrapper = mount(ResourceDetailView, {
      props: { resource, registry: 'team-tools', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: '' },
      global: { stubs },
    })

    expect(wrapper.get('code').text()).toBe(`agent-marketplace install "https://market.example/r/team-tools/skill/review/revisions/${encodeURIComponent(digest)}"`)
    expect(wrapper.find('button[aria-label^="Copy detail URL"]').exists()).toBe(false)
  })

  it('renders exact digest revision links and preserves revoked history state', async () => {
    const previousDigest = 'sha256:' + 'd'.repeat(64)
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource,
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
        revisions: [
          { ...resource, content_digest: previousDigest, revocation: { ...revocation, reason: 'policy' } },
        ],
      },
      global: { stubs },
    })

    await wrapper.findAll('[role="tab"]')[2]!.trigger('click')
    const history = wrapper.get('[aria-labelledby="revisions-tab"]')
    expect(history.get(`a[href="/r/registry/skill/review/revisions/${encodeURIComponent(previousDigest)}?tab=revisions"]`).exists()).toBe(true)
    expect(history.text()).toContain('Revoked')
    expect(history.text()).toContain('Latest')
    expect(history.get(`a[href="/r/registry/skill/review/revisions/${encodeURIComponent(digest)}?tab=revisions"]`).attributes('aria-current')).toBe('page')
  })

  it.each([
    ['an index-only skill', 'index-only', 'skill'],
    ['a collection', 'mirror', 'collection'],
  ] as const)('keeps an archive-free revision available for %s', async (_, acquisition, kind) => {
    const { archive_digest: _archiveDigest, ...withoutArchive } = resource
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: withoutArchive,
        registry: 'registry', kind, name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
        acquisition,
        revisions: [withoutArchive],
      },
      global: { stubs },
    })

    await wrapper.findAll('[role="tab"]')[2]!.trigger('click')
    const history = wrapper.get('[aria-labelledby="revisions-tab"]')
    expect(history.text()).toContain('Available')
    expect(history.text()).not.toContain('No installable artifact')
  })

  it('renders collection members as linked resources before exposing the raw manifest', async () => {
    const memberDigest = 'sha256:' + '8'.repeat(64)
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, coordinate: { ...coordinate, kind: 'collection', name: 'bundle' } },
        registry: 'registry', kind: 'collection', name: 'bundle', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
        inspection: {
          schema_version: 'v1', coordinate: { marketplace: 'marketplace', registry: 'registry', kind: 'collection', name: 'bundle' }, revision_digest: digest, primary_path: 'COLLECTION.json',
          files: [{ path: 'COLLECTION.json', content_digest: digest, media_type: 'application/json', size: 100, inspection_class: 'text' }],
        },
        primaryContent: JSON.stringify({ members: [{ kind: 'skill', name: 'review', registry_id: 'registry', revision: memberDigest }] }),
        relatedResources: [{ registry_id: 'registry', kind: 'skill', name: 'review', revision: memberDigest, content_digest: memberDigest, description: 'Reviews changes before merge.' }],
      },
      global: { stubs },
    })

    expect(wrapper.get('[data-testid="collection-members"]').text()).toContain('review')
    expect(wrapper.get('[data-testid="collection-members"]').text()).toContain('Skill')
    expect(wrapper.get('[data-testid="collection-members"]').text()).toContain('Reviews changes before merge.')
    expect(wrapper.get(`a[href="/r/registry/skill/review/revisions/${encodeURIComponent(memberDigest)}"]`).exists()).toBe(true)
    expect(wrapper.find('[data-testid="escaped-raw"]').exists()).toBe(false)

    await wrapper.get('[aria-label="Content view"]').findAll('button')[1]!.trigger('click')
    expect(wrapper.get('[data-testid="escaped-raw"]').text()).toContain('"members"')
  })

  it('renders only sanitized Markdown and an escaped raw view from the inspection projection', async () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, assessment: { ...assessmentBase, findings: [{ code: 'remote-reference', severity: 'warn', policy: 'network-policy', message: '<script>finding</script>', location: { path: 'SKILL.md', line: 1, column: 2 } }] } },
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
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

    await wrapper.get('[aria-label="Content view"]').findAll('button')[1]!.trigger('click')
    expect(wrapper.get('[data-testid="escaped-raw"]').html()).toContain('&lt;script&gt;')
    expect(wrapper.get('[data-testid="escaped-raw"] mark').attributes('aria-label')).toContain('remote-reference (warn)')

    await wrapper.findAll('[role="tab"]')[1]!.trigger('click')
    expect(wrapper.text()).toContain('View highlighted range')
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
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
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
    expect(wrapper.find('[aria-label="Content view"]').exists()).toBe(false)
    expect(wrapper.find('script').exists()).toBe(false)

    await selector.setValue('sha256:' + 'b'.repeat(64))
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('unavailable')
    expect(wrapper.find('script').exists()).toBe(false)
  })

  it('shows all-kind metadata without rendering SVG or binary bodies', async () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, coordinate: { ...resource.coordinate, kind: 'command', name: 'build' } },
        registry: 'registry', kind: 'command', name: 'build', canonicalUrl: 'https://market.example', browserBase: 'https://market.example', apiBase: '/',
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
    await wrapper.findAll('[role="tab"]')[4]!.trigger('click')
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

  // spec: marketplace-web-ui/other-unavailable-reasons-remain-concise
  it('summarizes revoked content without duplicate installation controls', () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, revocation },
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
      },
      global: { stubs },
    })

    const notice = wrapper.get('[data-testid="unavailable-notice"]')
    expect(notice.text()).toContain('Not available')
    expect(notice.text()).toContain('This revision has been revoked and can no longer be installed.')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('button[aria-label^="Copy pinned CLI"]').exists()).toBe(false)
    expect(wrapper.find('a[href="/docs/"]').exists()).toBe(false)
    expect(wrapper.find('button[aria-label^="Copy detail URL"]').exists()).toBe(false)
    expect(wrapper.findAll('button').filter(button => button.text() === 'View security details')).toHaveLength(0)
  })

  // spec: marketplace-web-ui/policy-blocked-resource-links-to-security-details
  it('calls a policy-rejected revision blocked and links to security details', async () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, archive_digest: undefined, assessment: { ...assessmentBase, decision: 'reject', findings: [{ code: 'shell-exec', severity: 'reject', policy: 'reject', message: 'executes a shell' }] } },
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
      },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('Blocked by policy')
    expect(wrapper.text()).not.toContain('Available')
    const notice = wrapper.get('[data-testid="unavailable-notice"]')
    expect(notice.text()).toContain('Not available')
    expect(notice.text()).toContain('This revision cannot be installed because it did not pass the Marketplace security policy.')
    expect(notice.text()).not.toMatch(/assessment|artifact|serving/i)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('code').exists()).toBe(false)
    expect(wrapper.find('button[aria-label^="Copy pinned CLI"]').exists()).toBe(false)
    expect(wrapper.find('a[href="/docs/"]').exists()).toBe(false)
    expect(wrapper.find('button[aria-label^="Copy detail URL"]').exists()).toBe(false)
    expect(wrapper.get('[aria-live="polite"]').text()).toContain('Install status for review: Blocked by policy.')

    const securityAction = notice.get('button')
    expect(securityAction.text()).toBe('View security details')
    await securityAction.trigger('click')
    expect(wrapper.get('#security-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[aria-labelledby="security-tab"]').text()).toContain('Automated scan')
  })

  it('reports a revision with no stored artifact as not installable without claiming a policy block', () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, archive_digest: undefined },
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
      },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('No installable artifact')
    expect(wrapper.text()).not.toContain('Blocked by policy')
    expect(wrapper.find('code').exists()).toBe(false)
    const notice = wrapper.get('[data-testid="unavailable-notice"]')
    expect(notice.text()).toContain('Not available')
    expect(notice.text()).toContain('This revision is available for inspection only and cannot be installed from this Marketplace.')
    expect(wrapper.find('button[aria-label^="Copy pinned CLI"]').exists()).toBe(false)
    expect(wrapper.find('a[href="/docs/"]').exists()).toBe(false)
  })

  it('gives each history row the status its own revision record supports', async () => {
    const rejectedDigest = 'sha256:' + 'd'.repeat(64)
    const strippedDigest = 'sha256:' + 'e'.repeat(64)
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource,
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
        revisions: [
          { ...resource, content_digest: rejectedDigest, archive_digest: undefined, assessment: { ...assessmentBase, decision: 'reject', findings: [{ code: 'shell-exec', severity: 'reject', policy: 'reject' }] } },
          { ...resource, content_digest: strippedDigest, archive_digest: undefined, assessment: { ...assessmentBase, findings: [] } },
        ],
      },
      global: { stubs },
    })

    await wrapper.findAll('[role="tab"]')[2]!.trigger('click')
    const rows = wrapper.get('[aria-labelledby="revisions-tab"]').findAll('li')
    expect(rows.map(row => row.text())).toEqual([
      expect.stringContaining('Blocked by policy'),
      expect.stringContaining('No installable artifact'),
      expect.stringContaining('Available'),
    ])
    expect(rows[2]?.text()).toContain('Latest')
  })

  it('uses assessment finding locations for highlighting', async () => {
    const wrapper = mount(ResourceDetailView, {
      props: {
        resource: { ...resource, assessment: { ...assessmentBase, findings: [{ code: 'remote-reference', severity: 'warn', policy: 'network-policy', message: 'references a remote host', location: { path: 'SKILL.md', line: 1, column: 2 } }] } },
        registry: 'registry', kind: 'skill', name: 'review', canonicalUrl: 'https://market.example', browserBase: 'https://market.example',
        inspection: {
          schema_version: 'v1', coordinate: { marketplace: 'marketplace', registry: 'registry', kind: 'skill', name: 'review' }, revision_digest: digest, primary_path: 'SKILL.md',
          files: [{ path: 'SKILL.md', content_digest: digest, media_type: 'text/markdown', size: 44, inspection_class: 'text' }],
        },
        primaryContent: '# Hello',
      },
      global: { stubs },
    })

    await wrapper.get('[aria-label="Content view"]').findAll('button')[1]!.trigger('click')
    expect(wrapper.get('[data-testid="escaped-raw"] mark').attributes('aria-label')).toContain('remote-reference')
    await wrapper.findAll('[role="tab"]')[1]!.trigger('click')
    expect(wrapper.findAll('[aria-label="Security findings"] > li')).toHaveLength(1)
    expect(wrapper.get('[aria-label="Security findings"]').text()).toContain('SKILL.md:1-2')
  })
})
