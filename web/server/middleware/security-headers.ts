import { createError, defineEventHandler, setResponseHeader } from 'h3'
import { useRuntimeConfig } from '#imports'
import { isAbsoluteApiBase, normalizeApiBase } from '~/lib/api'
import { basePolicy, generateCspNonce, scriptPolicy } from '../utils/security-policy'

export default defineEventHandler(event => {
  const config = useRuntimeConfig(event)
  const rawApiBase = String(config.public.apiBase ?? '/').trim()
  const apiBase = normalizeApiBase(rawApiBase)

  // A malformed absolute base must not silently fall back to same-origin. That
  // could make the UI display one endpoint while requests go to another.
  if (rawApiBase && rawApiBase !== '/' && !apiBase) {
    throw createError({ statusCode: 500, statusMessage: 'Invalid Marketplace API base configuration' })
  }

  const nonce = generateCspNonce()
  event.context.cspNonce = nonce
  const connectSources = ["'self'"]
  if (isAbsoluteApiBase(rawApiBase)) {
    connectSources.push(new URL(apiBase).origin)
  }

  setResponseHeader(event, 'Content-Security-Policy', [...basePolicy, scriptPolicy(nonce), `connect-src ${connectSources.join(' ')}`].join('; '))
  setResponseHeader(event, 'Referrer-Policy', 'strict-origin-when-cross-origin')
  setResponseHeader(event, 'X-Content-Type-Options', 'nosniff')
  setResponseHeader(event, 'X-Frame-Options', 'DENY')
  setResponseHeader(event, 'Permissions-Policy', 'camera=(), geolocation=(), microphone=()')
})
