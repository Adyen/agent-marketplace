import { describe, expect, it } from 'vitest'
import { isSafeRouteToken, resourceAsyncDataKey, revisionAsyncDataKey } from '../../app/lib/routes'

describe('safe marketplace route values', () => {
  it.each(['source-team', 'source.id', 'code review', 'sha256:' + 'a'.repeat(64)])('accepts route-safe token %s', value => {
    expect(isSafeRouteToken(value)).toBe(true)
  })

  it.each(['', 'source/team', 'source%2Fteam', '../source', '<script>', 'name"; rm -rf /', 'source\u0000id'])('rejects unsafe route token %s', value => {
    expect(isSafeRouteToken(value)).toBe(false)
  })

  it('uses collision-proof array serialization for resource keys', () => {
    expect(resourceAsyncDataKey('a-b', 'skill', 'c')).not.toBe(resourceAsyncDataKey('a', 'skill', 'b-c'))
    expect(revisionAsyncDataKey('source', 'skill', 'name', 'sha256:' + 'a'.repeat(64))).not.toBe(resourceAsyncDataKey('source', 'skill', 'name'))
    expect(resourceAsyncDataKey('source', 'skill', 'name')).toBe(JSON.stringify(['marketplace-resource', 'source', 'skill', 'name']))
  })
})
