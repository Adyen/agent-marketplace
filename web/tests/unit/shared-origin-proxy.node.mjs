import assert from 'node:assert/strict'
import { createServer, request } from 'node:http'
import { after, before, describe, it } from 'node:test'
import { createSharedOriginProxy } from '../e2e/shared-origin-proxy.mjs'

const servers = []
let proxyOrigin
let attackerOrigin
let attackerRequests = 0

async function listen(server) {
  servers.push(server)
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  return `http://127.0.0.1:${server.address().port}`
}

function get(path) {
  return new Promise((resolve, reject) => {
    const origin = new URL(proxyOrigin)
    const req = request({ hostname: origin.hostname, port: origin.port, path, headers: { host: 'untrusted.invalid' } }, (response) => {
      let body = ''
      response.setEncoding('utf8')
      response.on('data', (chunk) => { body += chunk })
      response.on('end', () => resolve({ body, headers: response.headers }))
    })
    req.on('error', reject)
    req.end()
  }).then(({ body, headers }) => {
    assert.equal(headers['content-type'], 'text/plain; charset=utf-8')
    assert.equal(headers['x-content-type-options'], 'nosniff')
    return body
  })
}

before(async () => {
  const upstream = name => createServer((req, res) => {
    res.setHeader('Content-Type', 'text/plain; charset=utf-8')
    res.setHeader('X-Content-Type-Options', 'nosniff')
    res.end(`${name}:${req.url}`)
  })
  const siteOrigin = await listen(upstream('site'))
  const marketplaceOrigin = await listen(upstream('marketplace'))
  const apiOrigin = await listen(upstream('api'))
  attackerOrigin = await listen(createServer((_req, res) => {
    attackerRequests++
    res.end('unintended upstream')
  }))
  proxyOrigin = await listen(createSharedOriginProxy({ siteOrigin, marketplaceOrigin, apiOrigin }))
})

after(async () => {
  await Promise.all(servers.map(server => new Promise((resolve, reject) => {
    server.close(error => error ? reject(error) : resolve())
    server.closeAllConnections()
  })))
})

describe('shared-origin test proxy', () => {
  it('preserves configured routing and query strings', async () => {
    assert.equal(await get('/docs/?q=notes'), 'site:/docs/?q=notes')
    assert.equal(await get('/marketplace/?q=notes'), 'marketplace:/marketplace/?q=notes')
    assert.equal(await get('/api/v1/search?q=notes'), 'api:/api/v1/search?q=notes')
  })

  it('does not resolve a network-path pathname as a new upstream origin', async () => {
    const path = `//${new URL(attackerOrigin).host}/unexpected?q=1`
    assert.equal(await get(`http://untrusted.invalid${path}`), `site:${path}`)
    assert.equal(attackerRequests, 0)
  })
})
