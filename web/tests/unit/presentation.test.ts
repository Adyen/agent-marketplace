import { describe, expect, it } from 'vitest'
import { installCommand, resourceLocator, safeCanonicalUrl } from '../../app/lib/presentation'

describe('copy-only resource actions', () => {
  it('creates the documented browser locator and CLI command', () => {
    const item = { source_id: 'source', source_slug: 'source/team', kind: 'skill' as const, name: 'code review', revision: 'v1', content_digest: 'sha256:' + 'a'.repeat(64) }
    const url = resourceLocator(item, 'https://market.example/')
    expect(url).toBe('https://market.example/s/source%2Fteam/skill/code%20review/revisions/sha256%3A' + 'a'.repeat(64))
    expect(installCommand(item, 'https://market.example')).toBe(`agent-marketplace install "${url}"`)
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
