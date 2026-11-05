import { afterEach, describe, expect, it, vi } from 'vitest'
import { marketplaceDocsUrl } from '../../app/lib/site'

describe('Marketplace documentation destination', () => {
  afterEach(() => { vi.unstubAllGlobals() })

  it('defaults the Nuxt runtime flag to false', async () => {
    vi.stubGlobal('defineNuxtConfig', <T>(config: T) => config)
    const { default: config } = await import('../../nuxt.config')
    expect(config.runtimeConfig?.public?.docsEnabled).toBe(false)
  })

  // spec: marketplace-web-ui/marketplace-header-offers-non-redundant-external-documentation-navigation
  it.each([undefined, null, false, 'false', '', '1'])('uses public documentation for %s', (enabled) => {
    expect(marketplaceDocsUrl(enabled)).toBe('https://agent-marketplace.io/docs')
  })

  // spec: marketplace-web-ui/marketplace-links-to-locally-hosted-documentation-when-enabled
  it.each([true, 'true'])('uses origin-root documentation for %s', (enabled) => {
    expect(marketplaceDocsUrl(enabled)).toBe('/docs/')
  })
})
