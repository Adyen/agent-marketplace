import { describe, expect, it } from 'vitest'
import { addNonceToScripts, basePolicy, generateCspNonce, scriptPolicy } from '../../server/utils/security-policy'

describe('runtime Content Security Policy', () => {
  it('allows only same-origin and nonce-authorized scripts', () => {
    expect(scriptPolicy('test-nonce')).toBe("script-src 'self' 'nonce-test-nonce'")
    expect(scriptPolicy('test-nonce')).not.toContain("'unsafe-inline'")
    expect(basePolicy).toContain("style-src 'self' 'unsafe-inline'")
    expect(basePolicy.some(directive => directive.startsWith('script-src'))).toBe(false)
  })

  it('generates a fresh nonce and adds it only to inline script tags', () => {
    const first = generateCspNonce()
    const second = generateCspNonce()
    expect(first).toMatch(/^[A-Za-z0-9_-]{22}$/)
    expect(second).not.toBe(first)

    const markup = '<script>hydrate()</script><script src="/_nuxt/entry.js"></script><script nonce="existing">keep()</script>'
    expect(addNonceToScripts(markup, first)).toBe(`<script nonce="${first}">hydrate()</script><script src="/_nuxt/entry.js"></script><script nonce="existing">keep()</script>`)
  })
})
