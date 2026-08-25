import { describe, expect, it } from 'vitest'
import { apiPath, createMarketplaceApi, isRasterMediaType, normalizeApiBase, resourceInspectionContentPath, resourceInspectionPath, resourcePath, resourceRevisionPath, resourceRevisionsPath, selectMarketplaceApiBase, selectMarketplaceFetcher, serializeSearchQuery, sourceHistoryPath, sourcePath, wellKnownPath } from '../../app/lib/api'
import { ApiValidationError, MAX_RESOURCE_REVISION_ENTRIES, MAX_SOURCE_HISTORY_ENTRIES, normalizeInspectionManifest, normalizeResourceRevision, normalizeResourceRevisions, normalizeSearchResult, normalizeSource, normalizeSourceHistory, normalizeWellKnown } from '../../app/lib/validation'
import type { ApiFetcher, SearchParams } from '../../app/types/marketplace'

const params: SearchParams = {
  q: 'code review & deploy',
  kind: 'skill',
  source: 'team/tools',
  offset: 24,
  limit: 24,
}

describe('Marketplace API URL handling', () => {
  it.each([
    ['/', ''],
    ['/api/v1', '/api/v1'],
    ['/api/v1/', '/api/v1'],
    ['https://market.example/api/v1/', 'https://market.example/api/v1'],
  ])('normalizes %s to %s', (input, output) => {
    expect(normalizeApiBase(input)).toBe(output)
  })

  it('does not duplicate the OpenAPI server prefix', () => {
    expect(apiPath('/api/v1', '/search')).toBe('/api/v1/search')
    expect(apiPath('/', '/search')).toBe('/api/v1/search')
    expect(apiPath('https://market.example/api/v1', '/search')).toBe('https://market.example/api/v1/search')
    expect(wellKnownPath('/api/v1')).toBe('/.well-known/agent-marketplace.json')
  })

  it('serializes supported search filters and pagination', () => {
    expect(serializeSearchQuery(params)).toEqual({
      q: 'code review & deploy',
      kind: 'skill',
      source: 'team/tools',
      offset: 24,
      limit: 24,
    })
  })

  it.each(['//evil.example', 'ftp://evil.example', 'https://user:pass@evil.example', 'https://market.example?x=1', 'https://market.example#x', '/api/v1?x=1', '/api/\u0000'])('bounds malformed API bases: %s', value => {
    expect(normalizeApiBase(value)).toBe('')
  })

  it('keeps the internal API base server-only', () => {
    expect(selectMarketplaceApiBase(true, '/', 'http://api:8080')).toBe('http://api:8080')
    expect(selectMarketplaceApiBase(false, '/', 'http://api:8080')).toBe('/')
    expect(selectMarketplaceApiBase(true, '/', '')).toBe('/')
  })

  it('uses request-bound fetch only for SSR relative API bases', () => {
    const requestFetcher: ApiFetcher = async <T>() => 'request' as T
    const clientFetcher: ApiFetcher = async <T>() => 'client' as T
    expect(selectMarketplaceFetcher(true, '/', requestFetcher, clientFetcher)).toBe(requestFetcher)
    expect(selectMarketplaceFetcher(true, 'https://api.example', requestFetcher, clientFetcher)).toBe(clientFetcher)
    expect(selectMarketplaceFetcher(false, '/', requestFetcher, clientFetcher)).toBe(clientFetcher)
  })

  it('builds the documented encoded resource route and validates API payloads', async () => {
    const digest = 'sha256:' + 'b'.repeat(64)
    expect(resourcePath('/api/v1', 'team/tools', 'skill', 'code review')).toBe('/api/v1/resources/team%2Ftools/skill/code%20review')
    expect(resourceRevisionPath('/api/v1', 'team/tools', 'skill', 'code review', digest)).toBe('/api/v1/resources/team%2Ftools/skill/code%20review/revisions/sha256%3A' + 'b'.repeat(64))
    const api = createMarketplaceApi(async () => ({
      coordinate: { marketplace: 'marketplace', source: 'team/tools', kind: 'skill', name: 'code review' },
      content_digest: digest,
    }))
    await expect(api.getResource('team/tools', 'skill', 'code review')).resolves.toMatchObject({ content_digest: digest })
  })

  it('lists bounded exact revisions and preserves revocation metadata', async () => {
    const latestDigest = 'sha256:' + 'a'.repeat(64)
    const revokedDigest = 'sha256:' + 'b'.repeat(64)
    expect(resourceRevisionsPath('/api/v1', 'team/tools', 'skill', 'code review')).toBe('/api/v1/resources/team%2Ftools/skill/code%20review/revisions')
    const payload = [
      { coordinate: { marketplace: 'marketplace', source: 'team/tools', kind: 'skill', name: 'code review' }, content_digest: latestDigest },
      { coordinate: { marketplace: 'marketplace', source: 'team/tools', kind: 'skill', name: 'code review' }, content_digest: revokedDigest, revocation: { reason: 'policy' } },
    ]
    const api = createMarketplaceApi(async request => request.endsWith('/revisions') ? payload : payload[0])
    await expect(api.listResourceRevisions('team/tools', 'skill', 'code review')).resolves.toMatchObject([
      { content_digest: latestDigest },
      { content_digest: revokedDigest, revocation: { reason: 'policy' } },
    ])
    expect(() => normalizeResourceRevisions(Array.from({ length: MAX_RESOURCE_REVISION_ENTRIES + 1 }, () => payload[0]), { kind: 'skill', name: 'code review' })).toThrow(/at most 100 entries/)
  })

  it('builds source discovery paths and validates source/history payloads', async () => {
    expect(sourcePath('/api/v1', 'team/tools')).toBe('/api/v1/sources/team%2Ftools')
    expect(sourceHistoryPath('/api/v1', 'team/tools')).toBe('/api/v1/sources/team%2Ftools/history')

    const sourcePayload = {
      id: 'source-id',
      slug: 'team-tools',
      aliases: ['old-team-tools'],
      origins: ['https://git.example/team/tools.git'],
      ref: 'main',
      roots: [{ path: 'skills', source_harness: 'factory' }],
      acquisition: 'mirror',
      publisher: { name: '<publisher>', url: '<not-a-link>' },
      license: 'MIT',
      listed: true,
    }
    const historyPayload = [{
      source_id: 'source-id',
      ref: 'main',
      commit: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      source_revision_manifest_digest: 'sha256:' + 'a'.repeat(64),
      acquisition: 'mirror',
      occurred_at: '2026-01-02T03:04:05Z',
    }]
    const api = createMarketplaceApi(async request => request.endsWith('/history') ? historyPayload : sourcePayload)
    await expect(api.getSource('team/tools')).resolves.toMatchObject({ slug: 'team-tools', publisher: { name: '<publisher>' } })
    await expect(api.getSourceHistory('team/tools')).resolves.toMatchObject([{ commit: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' }])
  })

  it('accepts only canonical source occurrence fields and digests', () => {
    const base = {
      source_id: 'source-id', ref: 'main', source_revision_manifest_digest: 'sha256:' + 'b'.repeat(64), acquisition: 'index-only', occurred_at: '2026-01-02T03:04:05Z',
    }
    const commit = 'c'.repeat(40)
    expect(normalizeSourceHistory([{ ...base, commit }])[0].commit).toBe(commit)
    expect(() => normalizeSourceHistory([{ ...base, commit: 'go-commit' }])).toThrow(/canonical Git commit/)
    expect(() => normalizeSourceHistory([{ ...base, revision_commit: 'legacy-commit' }])).toThrow(/revision_commit.*unsupported/)
    expect(() => normalizeSourceHistory([{ ...base, commit, revision_commit: 'legacy-commit' }])).toThrow(/revision_commit.*unsupported/)
    expect(() => normalizeSourceHistory([{ ...base, commit, source_revision_manifest_digest: 'sha256:manifest123' }])).toThrow(/SHA-256 digest/)
    expect(() => normalizeSourceHistory([{ ...base, commit, acquisition: 'index' }])).toThrow(/unsupported/)
    expect(() => normalizeSourceHistory(Array.from({ length: MAX_SOURCE_HISTORY_ENTRIES + 1 }, () => ({ ...base, commit })))).toThrow(/at most 1000 entries/)
  })

  it('bounds hostile source metadata and history fields', () => {
    expect(() => normalizeSource({ id: 'id', slug: 'slug', origins: ['x'.repeat(4097)], ref: 'main', roots: [], acquisition: 'mirror', listed: true })).toThrow(ApiValidationError)
    expect(() => normalizeSourceHistory([{ source_id: 'id', ref: 'main', commit: '<script>', source_revision_manifest_digest: 'digest', acquisition: 'mirror', occurred_at: 'not-a-date' }])).toThrow(ApiValidationError)
  })

  it('rejects a revision response whose digest or coordinate differs from the request', async () => {
    const requestedDigest = 'sha256:' + 'd'.repeat(64)
    const returnedDigest = 'sha256:' + 'e'.repeat(64)
    const api = createMarketplaceApi(async () => ({
      coordinate: { marketplace: 'marketplace', source: 'source', kind: 'skill', name: 'review' },
      content_digest: returnedDigest,
    }))

    await expect(api.getResourceRevision('source', 'skill', 'review', requestedDigest)).rejects.toThrow(/does not match requested digest/)

    expect(() => normalizeResourceRevision({
      coordinate: { marketplace: 'marketplace', source: 'other-source', kind: 'skill', name: 'review' },
      content_digest: requestedDigest,
    }, { sourceId: 'source', kind: 'skill', name: 'review', contentDigest: requestedDigest })).toThrow(/coordinate.source does not match/)

    expect(() => normalizeResourceRevision({
      coordinate: { source: 'source', kind: 'skill', name: 'review' },
      content_digest: requestedDigest,
    })).toThrow(/coordinate.marketplace/)
    expect(() => normalizeResourceRevision({
      coordinate: { marketplace: 'marketplace', source: 'source', kind: 'skill', name: 'review' },
      content_digest: requestedDigest,
      archive_digest: 'sha256:archive',
    })).toThrow(/archive_digest.*SHA-256 digest/)
    expect(() => normalizeResourceRevision({
      coordinate: { marketplace: 'marketplace', source: 'source', kind: 'rule', name: 'review' },
      content_digest: requestedDigest,
    })).toThrow(/coordinate.kind is unsupported/)
    expect(() => normalizeResourceRevision({
      coordinate: { marketplace: 'marketplace', source: 'source', kind: 'skill', name: 'review' },
      content_digest: requestedDigest,
      acquisition: 'index',
    })).toThrow(/acquisition is unsupported/)
  })

  it('selects text and raster response handling for inspection content', async () => {
    expect(isRasterMediaType('image/png')).toBe(true)
    expect(isRasterMediaType('application/json')).toBe(false)
    const calls: Array<{ request: string; responseType?: string }> = []
    const api = createMarketplaceApi(async <T>(request: string, options?: { responseType?: string }) => {
      calls.push({ request, responseType: options?.responseType })
      return 'body' as T
    }, '/api/v1')
    await api.getResourceInspectionContent('source', 'skill', 'review', 'sha256:' + 'a'.repeat(64), 'application/json')
    await api.getResourceInspectionFile('source', 'skill', 'review', 'sha256:' + 'a'.repeat(64), 'sha256:' + 'b'.repeat(64), 'image/png')
    expect(calls.map(call => call.responseType)).toEqual(['text', 'blob'])
  })

  it('builds exact inspection paths and validates bounded projection metadata', async () => {
    const digest = 'sha256:' + 'f'.repeat(64)
    expect(resourceInspectionPath('/api/v1', 'source', 'skill', 'review', digest)).toBe('/api/v1/resources/source/skill/review/revisions/sha256%3A' + 'f'.repeat(64) + '/inspection')
    expect(resourceInspectionContentPath('/api/v1', 'source', 'skill', 'review', digest)).toContain('/inspection/content')
    const payload = {
      schema_version: 'v1', coordinate: { marketplace: 'marketplace', source: 'source', kind: 'skill', name: 'review' }, revision_digest: digest, primary_path: 'SKILL.md',
      files: [{ path: 'SKILL.md', content_digest: digest, media_type: 'text/markdown', size: 10, inspection_class: 'text' }],
      findings: [{ code: 'warn', severity: 'warn', path: 'SKILL.md', start_byte: 1, end_byte: 2 }],
    }
    const api = createMarketplaceApi(async () => payload)
    await expect(api.getResourceInspection('source', 'skill', 'review', digest)).resolves.toMatchObject({ primary_path: 'SKILL.md' })
    expect(() => normalizeInspectionManifest({ ...payload, files: [{ ...payload.files[0], media_type: 'text/plain' }] })).toThrow(/primary media policy/)
    expect(() => normalizeInspectionManifest({ ...payload, files: [{ ...payload.files[0] }, { ...payload.files[0], path: 'support/COMMAND.md' }] })).toThrow(/path is not canonical/)
    expect(() => normalizeInspectionManifest({ ...payload, findings: [{ code: 'warn', severity: 'warn', start_byte: 3, end_byte: 2 }] })).toThrow(/inverted byte range/)
  })

  it('accepts API-only well-known metadata without browser templates', () => {
    const metadata = normalizeWellKnown({
      instance_id: 'api-only', canonical_url: 'https://api.example', api_base: '/api/v1',
      api_versions: ['v1'], artifact_schema_versions: ['v1'], resource_kinds: ['skill'],
      acquisition_modes: ['mirror'], features: [],
    })
    expect(metadata.browser_base).toBeUndefined()
    expect(metadata.browser_latest_route_template).toBeUndefined()
    expect(metadata.browser_exact_route_template).toBeUndefined()
    expect(() => normalizeWellKnown({
      instance_id: 'partial', canonical_url: 'https://api.example', api_base: '/api/v1', browser_base: 'https://web.example',
      api_versions: ['v1'], artifact_schema_versions: ['v1'], resource_kinds: ['skill'], acquisition_modes: ['mirror'], features: [],
    })).toThrow(/browser fields must be provided together/)
    expect(() => normalizeWellKnown({
      instance_id: 'mismatch', canonical_url: 'https://api.example', api_base: '/api/v1', browser_base: 'https://web.example',
      browser_latest_route_template: 'https://other.example/s/{source}/{kind}/{name}', browser_exact_route_template: 'https://web.example/s/{source}/{kind}/{name}/revisions/{revision}',
      api_versions: ['v1'], artifact_schema_versions: ['v1'], resource_kinds: ['skill'], acquisition_modes: ['mirror'], features: [],
    })).toThrow(/route templates do not match/)
  })

  it('turns malformed discovery payloads into bounded validation errors', () => {
    expect(() => normalizeWellKnown({ instance_id: 'x' })).toThrow(ApiValidationError)
    expect(() => normalizeWellKnown({ instance_id: 'x', canonical_url: 'https://example.com', api_base: '/api/v1', browser_base: 'https://example.com', browser_latest_route_template: 'https://example.com/s/{source}/{kind}/{name}', browser_exact_route_template: 'https://example.com/s/{source}/{kind}/{name}/revisions/{revision}', api_versions: [], artifact_schema_versions: ['v1'], resource_kinds: ['skill'], acquisition_modes: ['mirror'], features: [] })).toThrow(/api_versions must not be empty/)
    expect(() => normalizeSearchResult({ items: [{ name: '<unbounded>' }], total: 0, offset: 0, limit: 10 })).toThrow(ApiValidationError)
    expect(() => normalizeSearchResult({ items: [{ source_id: 'source', kind: 'skill', name: 'review', revision: 'refs/heads/main', content_digest: 'sha256:' + 'a'.repeat(64) }], total: 1, offset: 0, limit: 10 })).toThrow(/revision.*SHA-256 digest/)
    expect(() => normalizeSource({ id: 'id', slug: 'slug', origins: ['https://example.com/repo'], ref: 'main', roots: [], acquisition: 'mirror', listed: true })).toThrow(/roots must not be empty/)
    expect(() => normalizeSearchResult({ items: [], total: -1, offset: 0, limit: 10 })).toThrow(/bounded non-negative integer/)
  })
})
