import { describe, expect, it } from 'vitest'
import { isSafeRouteToken, resourceAsyncDataKey, resourceInspectionAsyncDataKey, resourceInspectionContentAsyncDataKey, revisionAsyncDataKey, registryAsyncDataKey, registryHistoryAsyncDataKey } from '../../app/lib/routes'

describe('safe marketplace route values', () => {
  it.each(['registry-team', 'registry.id', 'code review', 'sha256:' + 'a'.repeat(64)])('accepts route-safe token %s', value => {
    expect(isSafeRouteToken(value)).toBe(true)
  })

  it.each(['', 'registry/team', 'registry%2Fteam', '../registry', '<script>', 'name"; rm -rf /', 'registry\u0000id'])('rejects unsafe route token %s', value => {
    expect(isSafeRouteToken(value)).toBe(false)
  })

  it('uses collision-proof array serialization for resource and registry keys', () => {
    const revision = 'sha256:' + 'a'.repeat(64)
    expect(resourceAsyncDataKey('a-b', 'skill', 'c')).not.toBe(resourceAsyncDataKey('a', 'skill', 'b-c'))
    expect(revisionAsyncDataKey('registry', 'skill', 'name', revision)).not.toBe(resourceAsyncDataKey('registry', 'skill', 'name'))
    expect(resourceInspectionAsyncDataKey('a-b', 'skill', 'c', revision)).not.toBe(resourceInspectionAsyncDataKey('a', 'skill', 'b-c', revision))
    expect(resourceInspectionContentAsyncDataKey('a-b', 'skill', 'c', revision)).not.toBe(resourceInspectionContentAsyncDataKey('a', 'skill', 'b-c', revision))
    expect(resourceInspectionContentAsyncDataKey('registry', 'skill', 'name', revision)).not.toBe(resourceInspectionAsyncDataKey('registry', 'skill', 'name', revision))
    expect(registryAsyncDataKey('a-b')).not.toBe(registryHistoryAsyncDataKey('a-b'))
    expect(registryAsyncDataKey('registry')).toBe(JSON.stringify(['marketplace-registry', 'registry']))
    expect(registryHistoryAsyncDataKey('registry')).toBe(JSON.stringify(['marketplace-registry-history', 'registry']))
    expect(resourceInspectionAsyncDataKey('registry', 'skill', 'name', revision)).toBe(JSON.stringify(['marketplace-resource-inspection', 'registry', 'skill', 'name', revision]))
    expect(resourceInspectionContentAsyncDataKey('registry', 'skill', 'name', revision)).toBe(JSON.stringify(['marketplace-resource-inspection-content', 'registry', 'skill', 'name', revision]))
  })
})
