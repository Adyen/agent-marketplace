import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { mkdtempSync, rmSync, symlinkSync } from 'node:fs'
import { createServer, request } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { after, before, describe, it } from 'node:test'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'
import { createSharedOriginProxy } from '../e2e/shared-origin-proxy.mjs'

const servers = []
let origins
let proxyOrigin
let attackerOrigin
let attackerRequests = 0

async function listen(server) {
  servers.push(server)
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  return `http://127.0.0.1:${server.address().port}`
}

function get(path, { origin = proxyOrigin, host = 'untrusted.invalid' } = {}) {
  return new Promise((resolve, reject) => {
    const target = new URL(origin)
    const req = request({ hostname: target.hostname, port: target.port, path, headers: { host } }, (response) => {
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

async function unusedPort() {
  const server = createServer()
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  const { port } = server.address()
  await new Promise(resolve => server.close(resolve))
  return port
}

before(async () => {
  const upstream = name => createServer((req, res) => {
    res.setHeader('Content-Type', 'text/plain; charset=utf-8')
    res.setHeader('X-Content-Type-Options', 'nosniff')
    res.end(`${name}:${req.url}`)
  })
  origins = {
    siteOrigin: await listen(upstream('site')),
    marketplaceOrigin: await listen(upstream('marketplace')),
    apiOrigin: await listen(upstream('api')),
  }
  attackerOrigin = await listen(createServer((_req, res) => {
    attackerRequests++
    res.end('unintended upstream')
  }))
  proxyOrigin = await listen(createSharedOriginProxy(origins))
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
    assert.equal(await get('/?q=notes'), 'marketplace:/?q=notes')
    assert.equal(await get('/registries/example?q=notes'), 'marketplace:/registries/example?q=notes')
    assert.equal(await get('/api/v1/search?q=notes'), 'api:/api/v1/search?q=notes')
    assert.equal(await get('/.well-known/agent-marketplace.json'), 'api:/.well-known/agent-marketplace.json')
  })

  it('does not parse request paths against the Host header', async () => {
    assert.equal(await get('/docs/?q=notes', { host: 'untrusted.invalid:bad' }), 'site:/docs/?q=notes')
  })

  it('does not resolve a network-path pathname as a new upstream origin', async () => {
    const path = `//${new URL(attackerOrigin).host}/unexpected?q=1`
    assert.equal(await get(`http://untrusted.invalid${path}`), `marketplace:${path}`)
    assert.equal(attackerRequests, 0)
  })

  it('starts when executed through a symlinked path', async () => {
    const dir = mkdtempSync(join(tmpdir(), 'shared-origin-proxy-'))
    const link = join(dir, 'proxy.mjs')
    symlinkSync(fileURLToPath(new URL('../e2e/shared-origin-proxy.mjs', import.meta.url)), link)
    const port = await unusedPort()
    const child = spawn(process.execPath, [link], {
      env: {
        ...process.env,
        E2E_SHARED_ORIGIN_PORT: String(port),
        E2E_SITE_ORIGIN: origins.siteOrigin,
        E2E_ROOT_APP_ORIGIN: origins.marketplaceOrigin,
        E2E_API_ORIGIN: origins.apiOrigin,
      },
      stdio: 'ignore',
    })
    try {
      const deadline = Date.now() + 5_000
      let body
      while (body === undefined) {
        try {
          body = await get('/docs/', { origin: `http://127.0.0.1:${port}` })
        }
        catch (error) {
          if (error.code !== 'ECONNREFUSED' || Date.now() > deadline || child.exitCode !== null) throw error
          await delay(25)
        }
      }
      assert.equal(body, 'site:/docs/')
      assert.equal(await get('/', { origin: `http://127.0.0.1:${port}` }), 'marketplace:/')
    }
    finally {
      if (child.exitCode === null) {
        child.kill('SIGTERM')
        await once(child, 'exit')
      }
      rmSync(dir, { recursive: true, force: true })
    }
  })
})
