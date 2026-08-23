import { describe, expect, it } from 'vitest'
import { installCommand, resourceLocator, safeCanonicalUrl } from '../../app/lib/presentation'

describe('copy-only resource actions', () => {
  it('creates the documented browser locator and CLI command for route-safe values', () => {
    const item = { source_id: 'source-id', source_slug: 'source-team', kind: 'skill' as const, name: 'code review', revision: 'sha256:' + 'c'.repeat(64), content_digest: 'sha256:' + 'a'.repeat(64) }
    const url = resourceLocator(item, 'https://market.example/')
    expect(url).toBe('https://market.example/s/source-team/skill/code%20review/revisions/sha256%3A' + 'a'.repeat(64))
    expect(installCommand(item, 'https://market.example')).toBe(`agent-marketplace install "${url}"`)
  })

  it('does not generate a copy link for a source ID with an encoded slash', () => {
    const item = { source_id: 'source/id', kind: 'skill' as const, name: 'review', revision: 'sha256:' + 'c'.repeat(64), content_digest: 'sha256:' + 'a'.repeat(64) }
    expect(resourceLocator(item, 'https://market.example')).toBe('')
    expect(installCommand(item, 'https://market.example')).toBe('')
  })

  it.each([
    { source_id: 'source/id', source_slug: 'source-id' },
    { source_id: 'source-id', source_slug: 'source/team' },
    { source_id: 'source-id', source_slug: '<script>' },
    { source_id: 'source-id', source_slug: 'source-id', name: 'name/with/slash' },
  ])('does not generate a locator for unsafe API route values', overrides => {
    const item = { source_id: 'source-id', source_slug: 'source-id', kind: 'skill' as const, name: 'review', revision: 'sha256:' + 'c'.repeat(64), content_digest: 'sha256:' + 'a'.repeat(64), ...overrides }
    expect(resourceLocator(item, 'https://market.example')).toBe('')
    expect(installCommand(item, 'https://market.example')).toBe('')
  })

  it.each([
    'https://user:pass@market.example',
    'https://market.example/path?next=https://evil.example',
    'https://market.example/path#fragment',
    'https://market.example/$HOME',
    'https://market.example/<script>',
    'https://market.example/\u0000',
    '//market.example',
    'https://',
    'javascript:alert(1)',
  ])('rejects unsafe canonical URL %s', value => {
    expect(safeCanonicalUrl(value)).toBe('')
  })
})
