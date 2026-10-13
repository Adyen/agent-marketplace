import { createServer } from 'node:http'
import { pipeline } from 'node:stream'
import { pathToFileURL } from 'node:url'

const port = Number(process.env.E2E_SHARED_ORIGIN_PORT || 4323)
const siteOrigin = process.env.E2E_SITE_ORIGIN || 'http://127.0.0.1:4324'
const marketplaceOrigin = process.env.E2E_RELATIVE_APP_ORIGIN || 'http://127.0.0.1:4322'
const apiOrigin = process.env.E2E_API_ORIGIN || 'http://127.0.0.1:4311'

export function createSharedOriginProxy(origins = { siteOrigin, marketplaceOrigin, apiOrigin }) {
  function targetFor(pathname) {
    if (pathname === '/marketplace' || pathname.startsWith('/marketplace/') || pathname.startsWith('/_fonts/')) {
      return origins.marketplaceOrigin
    }
    if (pathname === '/.well-known/agent-marketplace.json' || pathname.startsWith('/api/')) {
      return origins.apiOrigin
    }
    return origins.siteOrigin
  }

  return createServer(async (request, response) => {
    try {
      const incomingURL = new URL(request.url || '/', 'http://127.0.0.1')
      const targetOrigin = targetFor(incomingURL.pathname)
      const upstreamURL = new URL(targetOrigin)
      upstreamURL.pathname = incomingURL.pathname
      upstreamURL.search = incomingURL.search
      const headers = new Headers()

      for (const [name, value] of Object.entries(request.headers)) {
        if (value !== undefined && name !== 'host') headers.set(name, Array.isArray(value) ? value.join(', ') : value)
      }
      headers.set('host', new URL(targetOrigin).host)

      const upstream = await fetch(upstreamURL, {
        method: request.method,
        headers,
        redirect: 'manual',
        body: request.method === 'GET' || request.method === 'HEAD' ? undefined : request,
        duplex: 'half',
      })

      response.statusCode = upstream.status
      upstream.headers.forEach((value, name) => {
        if (name !== 'content-encoding' && name !== 'content-length') response.setHeader(name, value)
      })

      if (!upstream.body || request.method === 'HEAD') {
        response.end()
        return
      }

      pipeline(upstream.body, response, (error) => {
        if (error && !response.destroyed) response.destroy(error)
      })
    }
    catch (error) {
      response.writeHead(502, { 'content-type': 'text/plain; charset=utf-8' })
      response.end(`Shared-origin test proxy failed: ${error instanceof Error ? error.message : 'unknown error'}`)
    }
  })
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const server = createSharedOriginProxy()
  server.listen(port, '127.0.0.1')

  function close() {
    server.close(() => process.exit(0))
  }

  process.on('SIGINT', close)
  process.on('SIGTERM', close)
}
