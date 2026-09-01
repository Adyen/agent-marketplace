import { describe, expect, it } from 'vitest'
import { fromResourceKindFilter, parseSearchQuery, toLocationQuery, toResourceKindFilter } from '../../app/composables/useSearchQuery'
import { ALL_RESOURCE_KINDS } from '../../app/types/marketplace'

describe('URL-backed search state', () => {
  it('parses bounded pagination and supported kinds', () => {
    expect(parseSearchQuery({ q: '  deploy ', kind: 'command', registry: ['core'], offset: '48', limit: '1000' })).toEqual({
      q: '  deploy ',
      kind: 'command',
      registry: 'core',
      offset: 48,
      limit: 100,
    })
  })

  it('drops unsafe or invalid kind values while retaining safe text', () => {
    expect(parseSearchQuery({ kind: '<img src=x onerror=alert(1)>', offset: '-1', limit: 'nope' })).toEqual({
      q: '',
      kind: '',
      registry: '',
      offset: 0,
      limit: 25,
    })
  })

  it('uses a non-empty UI sentinel while preserving empty as the public all-kinds value', () => {
    expect(toResourceKindFilter(parseSearchQuery({}).kind)).toBe(ALL_RESOURCE_KINDS)
    expect(toResourceKindFilter(parseSearchQuery({ kind: '' }).kind)).toBe(ALL_RESOURCE_KINDS)
    expect(fromResourceKindFilter(ALL_RESOURCE_KINDS)).toBe('')
    expect(fromResourceKindFilter('command')).toBe('command')
  })

  it('writes all user-visible search state back to the URL', () => {
    expect(toLocationQuery({ q: 'review', kind: 'skill', registry: 'core', offset: 12, limit: 24 })).toEqual({
      q: 'review',
      kind: 'skill',
      registry: 'core',
      offset: '12',
      limit: '24',
    })
    expect(toLocationQuery({ q: '', kind: fromResourceKindFilter(ALL_RESOURCE_KINDS), registry: '', offset: 0, limit: 25 })).toEqual({ limit: '25' })
  })
})
