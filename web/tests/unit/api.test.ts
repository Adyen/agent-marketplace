import { describe, expect, it } from 'vitest'
import { apiPath, createMarketplaceApi, normalizeApiBase, resourcePath, resourceRevisionPath, selectMarketplaceFetcher, serializeSearchQuery, wellKnownPath } from '../../app/lib/api'
import { ApiValidationError, normalizeSearchResult, normalizeWellKnown } from '../../app/lib/validation'
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
      coordinate: { source: 'team/tools', kind: 'skill', name: 'code review' },
      content_digest: digest,
    }))
    await expect(api.getResource('team/tools', 'skill', 'code review')).resolves.toMatchObject({ content_digest: digest })
  })

  it('rejects a revision response whose digest or coordinate differs from the request', async () => {
    const requestedDigest = 'sha256:' + 'd'.repeat(64)
    const returnedDigest = 'sha256:' + 'e'.repeat(64)
    const api = createMarketplaceApi(async () => ({
      coordinate: { source: 'source', kind: 'skill', name: 'review' },
      content_digest: returnedDigest,
    }))

    await expect(api.getResourceRevision('source', 'skill', 'review', requestedDigest)).rejects.toThrow(/does not match requested digest/)

    const mismatchedCoordinateApi = createMarketplaceApi(async () => ({
      coordinate: { source: 'other-source', kind: 'skill', name: 'review' },
      content_digest: requestedDigest,
    }))
    await expect(mismatchedCoordinateApi.getResourceRevision('source', 'skill', 'review', requestedDigest)).rejects.toThrow(/coordinate.source does not match/)
  })

  it('turns malformed discovery payloads into bounded validation errors', () => {
    expect(() => normalizeWellKnown({ instance_id: 'x' })).toThrow(ApiValidationError)
    expect(() => normalizeSearchResult({ items: [{ name: '<unbounded>' }], total: 0, offset: 0, limit: 10 })).toThrow(ApiValidationError)
    expect(() => normalizeSearchResult({ items: [], total: -1, offset: 0, limit: 10 })).toThrow(/bounded non-negative integer/)
  })
})
