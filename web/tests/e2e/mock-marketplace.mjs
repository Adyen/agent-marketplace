import { createServer } from 'node:http'

const port = Number(process.env.E2E_MOCK_PORT || 4311)
const appOrigin = process.env.E2E_APP_ORIGIN || 'http://127.0.0.1:4321'
const digest = `sha256:${'a'.repeat(64)}`
const hostileDescription = 'Deploy safely & review <script>alert("xss")</script>'

const items = [
  {
    source_id: 'team-tools',
    source_slug: 'team-tools',
    kind: 'skill',
    name: 'secure-review',
    revision: 'main',
    content_digest: digest,
    description: hostileDescription,
    metadata: { owner: 'platform' },
  },
  {
    source_id: 'team-tools',
    source_slug: 'team-tools',
    kind: 'command',
    name: 'deploy-preview',
    revision: 'main',
    content_digest: `sha256:${'b'.repeat(64)}`,
    description: 'Deploy a preview environment.',
  },
]

const source = {
  id: 'team-tools',
  slug: 'team-tools',
  aliases: [],
  origins: ['https://example.invalid/team-tools.git'],
  ref: 'main',
  roots: [{ path: 'skills' }],
  acquisition: 'mirror',
  publisher: { name: 'Platform Team' },
  license: 'MIT',
  listed: true,
}

const wellKnown = {
  instance_id: 'e2e-marketplace',
  canonical_url: appOrigin,
  api_base: `http://127.0.0.1:${port}/api/v1`,
  browser_route_template: '/s/{source}/{kind}/{name}',
  api_versions: ['v1'],
  artifact_schema_versions: ['v1'],
  resource_kinds: ['skill', 'command', 'subagent', 'hook', 'mcpserver', 'collection'],
  acquisition_modes: ['index-only', 'mirror'],
  features: ['search', 'source-history'],
}

function resource(item) {
  return {
    coordinate: { source: item.source_id, kind: item.kind, name: item.name },
    content_digest: item.content_digest,
    archive_digest: `sha256:${'c'.repeat(64)}`,
    analyzer_version: 'e2e-analyzer-1',
    acquisition: 'mirror',
    provenance: { observed_from: 'mock-marketplace' },
    findings: [],
    license: 'MIT',
  }
}

function sendJson(response, status, payload) {
  const body = JSON.stringify(payload)
  response.writeHead(status, {
    'Access-Control-Allow-Origin': appOrigin,
    'Access-Control-Allow-Methods': 'GET, OPTIONS',
    'Access-Control-Allow-Headers': 'content-type',
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': Buffer.byteLength(body),
  })
  response.end(body)
}

function decodeSegment(value) {
  try {
    return decodeURIComponent(value)
  } catch {
    return ''
  }
}

function search(url) {
  const q = (url.searchParams.get('q') || '').trim().toLowerCase()
  const kind = url.searchParams.get('kind') || ''
  const sourceID = (url.searchParams.get('source') || '').trim()
  const offset = Math.max(Number(url.searchParams.get('offset') || 0), 0)
  const limit = Math.min(Math.max(Number(url.searchParams.get('limit') || 12), 1), 100)
  const filtered = items.filter(item => {
    const matchesQuery = !q || `${item.name} ${item.description}`.toLowerCase().includes(q)
    return matchesQuery && (!kind || item.kind === kind) && (!sourceID || item.source_id === sourceID)
  })
  return { items: filtered.slice(offset, offset + limit), total: filtered.length, offset, limit }
}

const server = createServer((request, response) => {
  if (request.method === 'OPTIONS') {
    response.writeHead(204, { 'Access-Control-Allow-Origin': appOrigin, 'Access-Control-Allow-Methods': 'GET, OPTIONS' })
    response.end()
    return
  }
  if (request.method !== 'GET') {
    sendJson(response, 405, { error: 'method not allowed' })
    return
  }

  const url = new URL(request.url || '/', `http://127.0.0.1:${port}`)
  const parts = url.pathname.split('/').filter(Boolean).map(decodeSegment)
  if (url.pathname === '/.well-known/agent-marketplace.json') return sendJson(response, 200, wellKnown)
  if (parts.join('/') === 'api/v1/search') return sendJson(response, 200, search(url))
  if (parts.join('/') === 'api/v1/instance') return sendJson(response, 200, { id: 'e2e-operator', name: 'E2E Marketplace', canonical_url: appOrigin })
  if (parts.length === 4 && parts[0] === 'api' && parts[1] === 'v1' && parts[2] === 'sources') {
    return parts[3] === source.id ? sendJson(response, 200, source) : sendJson(response, 404, { error: 'source not found' })
  }
  if (parts.length === 5 && parts[0] === 'api' && parts[1] === 'v1' && parts[2] === 'sources' && parts[4] === 'history') {
    return parts[3] === source.id ? sendJson(response, 200, []) : sendJson(response, 404, { error: 'source not found' })
  }
  if (parts.length >= 6 && parts[0] === 'api' && parts[1] === 'v1' && parts[2] === 'resources') {
    const item = items.find(candidate => candidate.source_id === parts[3] && candidate.kind === parts[4] && candidate.name === parts[5])
    if (!item) return sendJson(response, 404, { error: 'resource not found' })
    if (parts[6] === 'revisions' && parts[7] !== item.content_digest) return sendJson(response, 404, { error: 'revision not found' })
    return sendJson(response, 200, resource(item))
  }
  sendJson(response, 404, { error: 'not found' })
})

server.listen(port, '127.0.0.1', () => {
  process.stdout.write(`Mock Marketplace listening on http://127.0.0.1:${port}\n`)
})
