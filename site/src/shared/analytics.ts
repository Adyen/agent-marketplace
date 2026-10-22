import type { HeadConfig } from 'vitepress'

export const CLOUDFLARE_BEACON_SRC = 'https://static.cloudflareinsights.com/beacon.min.js'

const CLOUDFLARE_TOKEN_PATTERN = /^[0-9a-f]{32}$/

export function analyticsHead(profile: 'public' | 'docs', token: string | undefined): HeadConfig[] {
  if (profile === 'docs' || !token) return []
  if (!CLOUDFLARE_TOKEN_PATTERN.test(token)) {
    throw new Error('SITE_CLOUDFLARE_ANALYTICS_TOKEN must be a 32-character lowercase hex Cloudflare Web Analytics token')
  }
  return [['script', { defer: '', src: CLOUDFLARE_BEACON_SRC, 'data-cf-beacon': JSON.stringify({ token }) }]]
}
