import { describe, expect, it } from 'vitest'
import { parseSearchQuery, toLocationQuery } from '../../app/composables/useSearchQuery'

describe('URL-backed search state', () => {
  it('parses bounded pagination and supported kinds', () => {
    expect(parseSearchQuery({ q: '  deploy ', kind: 'command', registry: ['core', 'community'], offset: '48', limit: '1000' })).toEqual({
      q: '  deploy ',
      kind: 'command',
      registry: ['core', 'community'],
      offset: 48,
      limit: 100,
    })
  })

  it('drops unsafe or invalid kind values while retaining safe text', () => {
    expect(parseSearchQuery({ kind: '<img src=x onerror=alert(1)>', offset: '-1', limit: 'nope' })).toEqual({
      q: '',
      kind: '',
      registry: [],
      offset: 0,
      limit: 25,
    })
  })

  it('writes all user-visible search state back to the URL', () => {
    expect(toLocationQuery({ q: 'review', kind: 'skill', registry: ['core', 'community'], offset: 12, limit: 24 })).toEqual({
      q: 'review',
      kind: 'skill',
      registry: ['core', 'community'],
      offset: '12',
      limit: '24',
    })
    expect(toLocationQuery({ q: '', kind: '', registry: [], offset: 0, limit: 25 })).toEqual({ limit: '25' })
  })
})
