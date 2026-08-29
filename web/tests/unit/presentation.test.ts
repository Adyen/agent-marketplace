import { describe, expect, it } from 'vitest'
import { assessmentFindings, FINGERPRINT_LEVELS, fingerprintCells, installCommand, listAvailabilityCopy, marketplaceInstallCommand, resourceAvailability, resourceAvailabilityCopy, resourceLatestLocator, resourceLocator, safeCanonicalUrl, shortDigest } from '../../app/lib/presentation'
import type { ResourceAvailability } from '../../app/lib/presentation'

describe('copy-only resource actions', () => {
  it('creates the documented browser locator and CLI command for route-safe values', () => {
    const item = { source_id: 'source-id', source_slug: 'source-team', kind: 'skill' as const, name: 'code review', revision: 'sha256:' + 'c'.repeat(64), content_digest: 'sha256:' + 'a'.repeat(64) }
    const url = resourceLocator(item, 'https://market.example/')
    expect(resourceLatestLocator(item, 'https://market.example')).toBe('https://market.example/s/source-team/skill/code%20review')
    expect(url).toBe('https://market.example/s/source-team/skill/code%20review/revisions/sha256%3A' + 'a'.repeat(64))
    expect(installCommand(item, 'https://market.example')).toBe(`agent-marketplace install "${url}"`)
  })

  it('builds an install command for this Marketplace endpoint', () => {
    expect(marketplaceInstallCommand('https://market.example/')).toBe('agent-marketplace install https://market.example')
    expect(marketplaceInstallCommand('https://market.example/path?token=secret')).toBe('')
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

describe('shortDigest', () => {
  it('keeps the algorithm prefix and a recognisable hex prefix', () => {
    expect(shortDigest('sha256:0e90224a1b2c3d4e5f' + '0'.repeat(46))).toBe('sha256:0e90224a1b2c…')
  })

  it('is short enough to sit on one card line and still distinguishes revisions', () => {
    const first = shortDigest('sha256:' + 'a'.repeat(64))
    const second = shortDigest('sha256:' + 'b'.repeat(64))
    expect(first).toHaveLength('sha256:'.length + 12 + 1)
    expect(first).not.toBe(second)
  })

  it.each([
    '',
    'sha256:short',
    'SHA256:' + 'a'.repeat(64),
    'sha512:' + 'a'.repeat(64),
    'not a digest',
  ])('returns %p unchanged when it is not a canonical sha256 digest', value => {
    expect(shortDigest(value)).toBe(value)
  })
})

describe('assessmentFindings', () => {
  it('preserves every authoritative assessment finding without merging duplicates', () => {
    const finding = { code: 'remote-reference', severity: 'warn', location: { path: 'SKILL.md', line: 4, column: 2 } }
    const details = assessmentFindings([finding, { ...finding }])
    expect(details).toHaveLength(2)
    expect(details[0]).toMatchObject({ index: 0, path: 'SKILL.md', line: 4, column: 2 })
    expect(details[1]).toMatchObject({ index: 1, path: 'SKILL.md' })
  })

  it('supports canonical assessment locations', () => {
    expect(assessmentFindings([{ code: 'x', severity: 'warn', location: { path: 'SKILL.md', line: 2, column: 3, start_byte: 3, end_byte: 5 } }])[0]).toMatchObject({ path: 'SKILL.md', line: 2, column: 3, startByte: 3, endByte: 5 })
  })

  it('returns an empty list for absent findings', () => {
    expect(assessmentFindings(undefined)).toEqual([])
  })
})

describe('resourceAvailability', () => {
  const assessment = (decision: 'allow' | 'warn' | 'reject', findings: Array<{ code: string; severity: string; policy?: string }> = []) => ({
    coordinate: { marketplace: 'market', source: 'source', kind: 'skill', name: 'review' },
    content_digest: 'sha256:' + 'a'.repeat(64), assessment_digest: 'sha256:' + 'b'.repeat(64), analyzer_version: 'v2',
    policy_digest: 'sha256:' + 'c'.repeat(64), decision, findings, inspection_manifest_digest: 'sha256:' + 'd'.repeat(64), assessed_at: '2026-01-01T00:00:00Z',
  })
  const archive = { archive_digest: 'sha256:' + 'a'.repeat(64), assessment: assessment('allow') }
  const rejectFinding = { code: 'shell-exec', severity: 'reject', policy: 'reject' }

  it('calls a revision available only when it is unrevoked, allowed or warned, and has an artifact', () => {
    expect(resourceAvailability(archive)).toBe('available')
    expect(resourceAvailabilityCopy(archive)).toMatchObject({ label: 'Available', color: 'success', installable: true, installNotice: '' })
  })

  it('reports revocation ahead of every other signal', () => {
    const revision = { ...archive, assessment: assessment('reject', [rejectFinding]), revocation: { kind: 'revision' as const, target_id: 'digest', reason: 'security', applied_at: '2026-01-01T00:00:00Z' } }
    expect(resourceAvailability(revision)).toBe('revoked')
    expect(resourceAvailabilityCopy(revision).label).toBe('Revoked')
  })

  it('uses the assessment decision rather than finding severity as policy authority', () => {
    expect(resourceAvailability({ ...archive, assessment: assessment('reject') })).toBe('policy-blocked')
    expect(resourceAvailability({ ...archive, assessment: assessment('allow', [rejectFinding]) })).toBe('available')
  })

  it('reports a missing mirror artifact without a reject decision as unavailable', () => {
    const revision = { assessment: assessment('warn', [rejectFinding]) }
    expect(resourceAvailability(revision, 'mirror', 'skill')).toBe('no-artifact')
    expect(resourceAvailabilityCopy(revision, 'mirror', 'skill')).toMatchObject({ label: 'No installable artifact', color: 'warning', installable: false })
  })

  it('does not require archive digests for index-only resources or collections', () => {
    const revision = { assessment: assessment('allow') }
    expect(resourceAvailability(revision, 'index-only', 'skill')).toBe('available')
    expect(resourceAvailability(revision, 'mirror', 'collection')).toBe('available')
  })

  const blockedStates: Array<[ResourceAvailability, Parameters<typeof resourceAvailability>[0]]> = [
    ['revoked', { ...archive, revocation: { kind: 'revision', target_id: 'digest', reason: 'security', applied_at: '2026-01-01T00:00:00Z' } }],
    ['policy-blocked', { ...archive, assessment: assessment('reject') }],
    ['no-artifact', { assessment: assessment('allow') }],
  ]

  it.each(blockedStates)('never offers an install command in the %s state', (state, revision) => {
    expect(resourceAvailability(revision)).toBe(state)
    const copy = resourceAvailabilityCopy(revision)
    expect(copy.installable).toBe(false)
    expect(copy.installNotice).toContain('Installation is disabled')
    expect(copy.installNotice).toContain('Metadata and inspection remain available.')
  })
})

describe('listAvailabilityCopy', () => {
  it('reuses the revision copy for the states the listing shares with the resource page', () => {
    expect(listAvailabilityCopy({ availability: 'available' })).toMatchObject({ label: 'Available', installable: true })
    expect(listAvailabilityCopy({ availability: 'revoked' })).toMatchObject({ label: 'Revoked', installable: false })
  })

  it('states the outcome for the collapsed blocked state without guessing the reason', () => {
    const copy = listAvailabilityCopy({ availability: 'blocked' })
    expect(copy).toMatchObject({ label: 'Not installable', installable: false })
    // The listing cannot tell policy rejection from a missing artifact, so the
    // notice must not claim either one.
    expect(copy?.installNotice).not.toContain('policy')
    expect(copy?.installNotice).toContain('Open the resource')
  })

  it('returns null for an unreported status so a caller never renders "available" by default', () => {
    expect(listAvailabilityCopy({})).toBeNull()
    expect(listAvailabilityCopy({ availability: undefined })).toBeNull()
  })

  it('returns null for a status a newer server may add', () => {
    expect(listAvailabilityCopy({ availability: 'quarantined' as never })).toBeNull()
  })
})

describe('fingerprintCells', () => {
  const digest = (hex: string) => 'sha256:' + hex.padEnd(64, '0')

  it('derives nine quantized ink weights from the leading bytes of the digest', () => {
    // 0x00 -> 0 % 4 -> level 0; 0xff -> 255 % 4 -> level 3; 0x80 -> 128 % 4 -> level 0.
    const cells = fingerprintCells(digest('00ff80'))
    expect(cells).toHaveLength(9)
    expect(cells[0]).toBe(FINGERPRINT_LEVELS[0])
    expect(cells[1]).toBe(FINGERPRINT_LEVELS[3])
    expect(cells[2]).toBe(FINGERPRINT_LEVELS[0])
  })

  it('only ever emits one of the declared levels, so no cell is a mid-grey mush', () => {
    const cells = fingerprintCells(digest('0123456789abcdef01'))
    for (const weight of cells) {
      expect(FINGERPRINT_LEVELS).toContain(weight)
    }
  })

  it('is deterministic: the same content always produces the same mark', () => {
    expect(fingerprintCells(digest('abc123'))).toEqual(fingerprintCells(digest('abc123')))
  })

  it('produces a different mark when the content changes', () => {
    expect(fingerprintCells(digest('abc123'))).not.toEqual(fingerprintCells(digest('abc124')))
  })

  it('renders no mark for anything that is not a canonical digest', () => {
    // An invented mark would imply an identity the instance never reported.
    expect(fingerprintCells('')).toEqual([])
    expect(fingerprintCells('refs/heads/main')).toEqual([])
    expect(fingerprintCells('sha256:nothex')).toEqual([])
    expect(fingerprintCells('md5:' + 'a'.repeat(64))).toEqual([])
  })
})
