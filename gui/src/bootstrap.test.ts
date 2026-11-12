// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest'
import { resolveBootstrap } from './bootstrap'

afterEach(() => {
  delete window.go
  delete window.__AM_GUI_BOOTSTRAP__
})

describe('resolveBootstrap', () => {
  it('returns an empty base URL for the native-bound bootstrap', async () => {
    window.go = {
      main: {
        App: {
          Bootstrap: async () => ({ credential: 'native-token', packageFormat: 'rpm', version: '1.2.3' }),
        },
      },
    }
    const bootstrap = await resolveBootstrap()
    expect(bootstrap).toEqual({ url: '', credential: 'native-token', packageFormat: 'rpm', version: '1.2.3' })
  })

  it('uses the injected test bootstrap when no native bridge exists', async () => {
    window.__AM_GUI_BOOTSTRAP__ = { url: 'http://127.0.0.1:7777', credential: 'test-token' }
    const bootstrap = await resolveBootstrap()
    expect(bootstrap.url).toBe('http://127.0.0.1:7777')
    expect(bootstrap.credential).toBe('test-token')
    expect(bootstrap.packageFormat).toBe('')
  })

  it('fails truthfully when no trusted bootstrap exists', async () => {
    await expect(resolveBootstrap()).rejects.toThrow('no trusted bootstrap')
  })
})
