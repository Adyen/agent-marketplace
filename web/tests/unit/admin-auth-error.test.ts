import { describe, expect, it } from 'vitest'
import { resolveAdminAuthError } from '~/lib/admin-auth-error'

// spec: marketplace-admin-auth/failure-page-renders-without-a-session
// spec: marketplace-admin-auth/recovery-starts-a-new-login-with-a-safe-destination
describe('resolveAdminAuthError', () => {
  it('maps each supported code to fixed copy', () => {
    const expected: Record<string, { title: string, retry: boolean }> = {
      authentication_rejected: { title: "Sign-in wasn't completed", retry: true },
      authentication_unavailable: { title: 'Sign-in is temporarily unavailable', retry: true },
      authentication_request_invalid: { title: "Sign-in request wasn't accepted", retry: true },
      forbidden: { title: 'Administration access required', retry: false },
      admin_unavailable: { title: 'Administration sign-in is not configured', retry: false },
      origin_rejected: { title: 'Sign-in must start from the Marketplace', retry: false },
    }
    for (const [code, want] of Object.entries(expected)) {
      const resolved = resolveAdminAuthError({ code })
      expect(resolved.code, code).toBe(code)
      expect(resolved.title, code).toBe(want.title)
      expect(resolved.retry, code).toBe(want.retry)
      expect(resolved.message.length, code).toBeGreaterThan(0)
    }
  })

  it('falls back to safe generic copy for unknown codes', () => {
    for (const query of [
      {},
      { code: 'state' },
      { code: 'unauthenticated' },
      { code: 'constructor' },
      { code: '__proto__' },
      { code: 'hasOwnProperty' },
      { code: 'authentication_rejected<script>' },
      { code: 42 },
      { code: null },
      { code: ['authentication_rejected', 'forbidden'] },
      { code: ['forbidden'] },
      { code: {} },
    ]) {
      const resolved = resolveAdminAuthError(query)
      expect(resolved.code, JSON.stringify(query)).toBe('sign_in_failed')
      expect(resolved.title, JSON.stringify(query)).toBe("Sign-in couldn't be completed")
      expect(resolved.retry, JSON.stringify(query)).toBe(true)
    }
  })

  it('never reflects query values into copy', () => {
    const resolved = resolveAdminAuthError({ code: 'leak-marker', error_description: 'provider-secret', state: 'stolen-state' })
    for (const text of [resolved.code, resolved.title, resolved.message]) {
      expect(text).not.toContain('leak-marker')
      expect(text).not.toContain('provider-secret')
      expect(text).not.toContain('stolen-state')
    }
  })

  it('keeps only validated admin destinations on the retry link', () => {
    const valid = resolveAdminAuthError({ code: 'authentication_rejected', next: '/marketplace/admin/registries/team' })
    expect(valid.loginHref).toBe('/api/v1/admin/auth/login?next=%2Fmarketplace%2Fadmin%2Fregistries%2Fteam')

    const root = '/api/v1/admin/auth/login?next=%2Fmarketplace%2Fadmin'
    for (const next of [
      undefined,
      '',
      'https://evil.test/x',
      '//evil.test/x',
      '/marketplace/admin/../secret',
      '/marketplace/admin/x?y=1',
      '/marketplace/admin/x%2e%2e',
      '/marketplace/admin\\x',
      '/other',
      ['x', 'y'],
      42,
      null,
    ]) {
      const resolved = resolveAdminAuthError({ code: 'authentication_rejected', next })
      expect(resolved.loginHref, JSON.stringify(next)).toBe(root)
    }
    expect(valid.loginHref).not.toContain('callback')
  })

  it('rejects control characters and byte-overlong destinations on the retry link', () => {
    const parse = (href: string) => new URL(href, 'http://marketplace.test').searchParams.get('next')
    for (const next of [
      `/marketplace/admin/x${'\x00'}y`,
      `/marketplace/admin/x${'\x07'}y`,
      `/marketplace/admin/x${'\x1f'}y`,
      `/marketplace/admin/x${'\x7f'}y`,
      `/marketplace/admin/${'a'.repeat(2030)}`,
      `/marketplace/admin/${'é'.repeat(1015)}`,
    ]) {
      const resolved = resolveAdminAuthError({ code: 'authentication_rejected', next })
      expect(parse(resolved.loginHref), JSON.stringify(next.slice(0, 40))).toBe('/marketplace/admin')
    }
    for (const next of [`/marketplace/admin/${'a'.repeat(2029)}`, `/marketplace/admin/${'é'.repeat(1014)}x`]) {
      const resolved = resolveAdminAuthError({ code: 'authentication_rejected', next })
      expect(parse(resolved.loginHref)).toBe(next)
    }
  })

  it('honors a configured browser base for destinations and stays relative', () => {
    const kept = resolveAdminAuthError({ code: 'authentication_rejected', next: '/base/admin/x' }, '/base/')
    expect(kept.loginHref).toBe('/api/v1/admin/auth/login?next=%2Fbase%2Fadmin%2Fx')
    const otherBase = resolveAdminAuthError({ code: 'authentication_rejected', next: '/marketplace/admin/x' }, '/base/')
    expect(otherBase.loginHref).toBe('/api/v1/admin/auth/login?next=%2Fbase%2Fadmin')
  })
})
