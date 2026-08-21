import { describe, expect, it } from 'vitest'
import { isSafeRouteToken, resourceAsyncDataKey, revisionAsyncDataKey, sourceAsyncDataKey, sourceHistoryAsyncDataKey } from '../../app/lib/routes'

describe('safe marketplace route values', () => {
  it.each(['source-team', 'source.id', 'code review', 'sha256:' + 'a'.repeat(64)])('accepts route-safe token %s', value => {
    expect(isSafeRouteToken(value)).toBe(true)
  })

  it.each(['', 'source/team', 'source%2Fteam', '../source', '<script>', 'name"; rm -rf /', 'source\u0000id'])('rejects unsafe route token %s', value => {
    expect(isSafeRouteToken(value)).toBe(false)
  })

  it('uses collision-proof array serialization for resource and source keys', () => {
    expect(resourceAsyncDataKey('a-b', 'skill', 'c')).not.toBe(resourceAsyncDataKey('a', 'skill', 'b-c'))
    expect(revisionAsyncDataKey('source', 'skill', 'name', 'sha256:' + 'a'.repeat(64))).not.toBe(resourceAsyncDataKey('source', 'skill', 'name'))
    expect(sourceAsyncDataKey('a-b')).not.toBe(sourceHistoryAsyncDataKey('a-b'))
    expect(sourceAsyncDataKey('source')).toBe(JSON.stringify(['marketplace-source', 'source']))
    expect(sourceHistoryAsyncDataKey('source')).toBe(JSON.stringify(['marketplace-source-history', 'source']))
  })
})
