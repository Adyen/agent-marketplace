import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { validateSourceInput } from './source-input'

interface GitLocationCase {
  name: string
  location: string
  valid: boolean
}

interface MarketplaceLocationCase {
  name: string
  location: string
  admitted: boolean
  clientValid: boolean
}

const gitLocations = JSON.parse(
  readFileSync(new URL('../tests/fixtures/source-locations.json', import.meta.url), 'utf8'),
) as GitLocationCase[]

const marketplaceLocations = JSON.parse(
  readFileSync(new URL('../tests/fixtures/marketplace-locations.json', import.meta.url), 'utf8'),
) as MarketplaceLocationCase[]

describe('source input validation', () => {
  // spec: gui-configuration/valid-source-input-enables-addition
  // spec: gui-configuration/invalid-source-input-disables-addition
  it.each(gitLocations)('matches GUI add-source admission for $name', ({ location, valid }) => {
    const errors = validateSourceInput({ kind: 'git', location, ref: 'main', path: '' })
    expect(Object.hasOwn(errors, 'location')).toBe(!valid)
  })

  // spec: gui-configuration/valid-source-input-enables-addition
  // spec: gui-configuration/invalid-source-input-disables-addition
  it.each(marketplaceLocations)('matches Marketplace location validity for $name', ({ location, admitted, clientValid }) => {
    const errors = validateSourceInput({ kind: 'marketplace', location, ref: '', path: '' })
    expect(!Object.hasOwn(errors, 'location')).toBe(clientValid)
    if (admitted) expect(clientValid).toBe(true)
  })

  // spec: gui-configuration/invalid-source-input-disables-addition
  // spec: gui-configuration/valid-source-input-enables-addition
  it.each([
    { name: 'scheme without delimiter', location: 'https:market.example', valid: false },
    { name: 'single slash after scheme', location: 'https:/market.example', valid: false },
    { name: 'empty authority', location: 'https:///market.example', valid: false },
    { name: 'backslash in place of scheme delimiter', location: 'https:\\market.example', valid: false },
    { name: 'backslash at the start of authority', location: 'https://\\market.example', valid: false },
    { name: 'upper-case HTTP scheme', location: 'HTTP://market.example/api', valid: true },
    { name: 'upper-case HTTPS scheme', location: 'HTTPS://market.example/api', valid: true },
  ])('$name Marketplace URL validity', ({ location, valid }) => {
    const errors = validateSourceInput({ kind: 'marketplace', location, ref: '', path: '' })
    expect(Object.hasOwn(errors, 'location')).toBe(!valid)
  })

  // spec: gui-configuration/invalid-source-input-disables-addition
  it('returns field messages for invalid source inputs', () => {
    expect(validateSourceInput({ kind: 'directory', location: ' \t ', ref: '', path: '' })).toEqual({
      location: expect.any(String),
    })
    expect(validateSourceInput({ kind: 'git', location: 'https://example.com/repo.git', ref: ' \n', path: '' })).toEqual({})
    expect(validateSourceInput({ kind: 'marketplace', location: '/api', ref: '', path: '' })).toEqual({
      location: expect.any(String),
    })
    expect(validateSourceInput({ kind: 'marketplace', location: 'ftp://market.example', ref: '', path: '' })).toEqual({
      location: expect.any(String),
    })
  })

  // spec: gui-configuration/valid-source-input-enables-addition
  it('accepts valid inputs for each source kind', () => {
    expect(validateSourceInput({ kind: 'directory', location: ' /var/registries ', ref: '', path: '' })).toEqual({})
    expect(validateSourceInput({ kind: 'git', location: 'https://example.com/repo.git', ref: 'main', path: '' })).toEqual({})
    expect(validateSourceInput({ kind: 'marketplace', location: 'https://market.example/api', ref: '', path: '' })).toEqual({})
  })
})
