import { describe, expect, it } from 'vitest'
import { isSafeRouteToken, resourceAsyncDataKey, resourceInspectionAsyncDataKey, resourceInspectionContentAsyncDataKey, revisionAsyncDataKey, sourceAsyncDataKey, sourceHistoryAsyncDataKey } from '../../app/lib/routes'

describe('safe marketplace route values', () => {
  it.each(['source-team', 'source.id', 'code review', 'sha256:' + 'a'.repeat(64)])('accepts route-safe token %s', value => {
    expect(isSafeRouteToken(value)).toBe(true)
  })

  it.each(['', 'source/team', 'source%2Fteam', '../source', '<script>', 'name"; rm -rf /', 'source\u0000id'])('rejects unsafe route token %s', value => {
    expect(isSafeRouteToken(value)).toBe(false)
  })

  it('uses collision-proof array serialization for resource and source keys', () => {
    const revision = 'sha256:' + 'a'.repeat(64)
    expect(resourceAsyncDataKey('a-b', 'skill', 'c')).not.toBe(resourceAsyncDataKey('a', 'skill', 'b-c'))
    expect(revisionAsyncDataKey('source', 'skill', 'name', revision)).not.toBe(resourceAsyncDataKey('source', 'skill', 'name'))
    expect(resourceInspectionAsyncDataKey('a-b', 'skill', 'c', revision)).not.toBe(resourceInspectionAsyncDataKey('a', 'skill', 'b-c', revision))
    expect(resourceInspectionContentAsyncDataKey('a-b', 'skill', 'c', revision)).not.toBe(resourceInspectionContentAsyncDataKey('a', 'skill', 'b-c', revision))
    expect(resourceInspectionContentAsyncDataKey('source', 'skill', 'name', revision)).not.toBe(resourceInspectionAsyncDataKey('source', 'skill', 'name', revision))
    expect(sourceAsyncDataKey('a-b')).not.toBe(sourceHistoryAsyncDataKey('a-b'))
    expect(sourceAsyncDataKey('source')).toBe(JSON.stringify(['marketplace-source', 'source']))
    expect(sourceHistoryAsyncDataKey('source')).toBe(JSON.stringify(['marketplace-source-history', 'source']))
    expect(resourceInspectionAsyncDataKey('source', 'skill', 'name', revision)).toBe(JSON.stringify(['marketplace-resource-inspection', 'source', 'skill', 'name', revision]))
    expect(resourceInspectionContentAsyncDataKey('source', 'skill', 'name', revision)).toBe(JSON.stringify(['marketplace-resource-inspection-content', 'source', 'skill', 'name', revision]))
  })
})
