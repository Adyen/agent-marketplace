import { createServer } from 'node:http'

const port = Number(process.env.E2E_MOCK_PORT || 4311)
const appOrigin = process.env.E2E_APP_ORIGIN || 'http://127.0.0.1:4321'
const digest = `sha256:${'a'.repeat(64)}`
const hostileDescription = 'Deploy safely & review <script>alert("xss")</script>'
const inspectionDigest = `sha256:${'d'.repeat(64)}`
const primaryDigest = `sha256:${'e'.repeat(64)}`
const notesDigest = `sha256:${'f'.repeat(64)}`
const configDigest = `sha256:${'1'.repeat(64)}`
const primaryContent = '# Safe review\n\n[docs](https://example.com/docs)\n\n![remote image](https://evil.invalid/pixel.png)\n\n<div>raw HTML</div>\n<script>alert("xss")</script>\n'
const notesContent = '# Additional notes\n\n<script>alert("support-xss")</script>\n'
const configContent = '{"html":"<script>escaped</script>"}'

const items = [
  {
    source_id: 'team-tools',
    source_slug: 'team-tools',
    kind: 'skill',
    name: 'secure-review',
    revision: digest,
    content_digest: digest,
    description: hostileDescription,
    metadata: { owner: 'platform' },
  },
  {
    source_id: 'team-tools',
    source_slug: 'team-tools',
    kind: 'command',
    name: 'deploy-preview',
    revision: `sha256:${'b'.repeat(64)}`,
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
  browser_base: appOrigin,
  browser_latest_route_template: `${appOrigin}/s/{source}/{kind}/{name}`,
  browser_exact_route_template: `${appOrigin}/s/{source}/{kind}/{name}/revisions/{revision}`,
  api_versions: ['v1'],
  artifact_schema_versions: ['v1'],
  resource_kinds: ['skill', 'command', 'subagent', 'hook', 'mcpserver', 'collection'],
  acquisition_modes: ['index-only', 'mirror'],
  features: ['search', 'source-history', 'inspection'],
}

function resource(item, contentDigest = item.content_digest, revocation) {
  const effectiveRevocation = revocation === undefined && item.name === 'deploy-preview' ? { reason: 'security review' } : revocation
  return {
    coordinate: { marketplace: 'e2e-marketplace', source: item.source_id, kind: item.kind, name: item.name },
    content_digest: contentDigest,
    archive_digest: `sha256:${'c'.repeat(64)}`,
    analyzer_version: 'e2e-analyzer-1',
    acquisition: 'mirror',
    provenance: { observed_from: 'mock-marketplace', inspection_manifest_digest: inspectionDigest },
    findings: [],
    license: 'MIT',
    ...(effectiveRevocation ? { revocation: effectiveRevocation } : {}),
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
    const previousDigest = `sha256:${'9'.repeat(64)}`
    const requestedRevision = parts[6] === 'revisions' ? (parts[7] || item.content_digest) : item.content_digest
    if (parts[6] === 'revisions' && parts[7] === undefined) {
      return sendJson(response, 200, [resource(item), resource(item, previousDigest, item.name === 'secure-review' ? { reason: 'superseded policy' } : undefined)])
    }
    if (parts[6] === 'revisions' && ![item.content_digest, previousDigest].includes(requestedRevision)) return sendJson(response, 404, { error: 'revision not found' })
    if (parts[8] === 'inspection') {
      if (parts[9] === 'content') {
        if (parts[10] === notesDigest) {
          response.writeHead(200, { 'Access-Control-Allow-Origin': appOrigin, 'Content-Type': 'text/markdown', 'X-Content-Type-Options': 'nosniff' })
          return response.end(notesContent)
        }
        if (parts[10] === configDigest) {
          response.writeHead(200, { 'Access-Control-Allow-Origin': appOrigin, 'Content-Type': 'application/json', 'X-Content-Type-Options': 'nosniff' })
          return response.end(configContent)
        }
        response.writeHead(200, { 'Access-Control-Allow-Origin': appOrigin, 'Content-Type': 'text/markdown', 'X-Content-Type-Options': 'nosniff' })
        return response.end(primaryContent)
      }
      return sendJson(response, 200, { schema_version: 'v1', coordinate: { marketplace: 'e2e-marketplace', source: item.source_id, kind: item.kind, name: item.name }, revision_digest: requestedRevision, primary_path: 'SKILL.md', files: [
        { path: 'SKILL.md', content_digest: primaryDigest, media_type: 'text/markdown', size: Buffer.byteLength(primaryContent), inspection_class: 'text' },
        { path: 'support/notes.md', content_digest: notesDigest, media_type: 'text/markdown', size: Buffer.byteLength(notesContent), inspection_class: 'text' },
        { path: 'support/config.json', content_digest: configDigest, media_type: 'application/json', size: Buffer.byteLength(configContent), inspection_class: 'text' },
        { path: 'support/tool.bin', content_digest: `sha256:${'2'.repeat(64)}`, media_type: 'application/octet-stream', size: 4, inspection_class: 'metadata' },
      ], findings: [{ code: 'remote-reference', severity: 'warn', policy: 'network-policy', path: 'SKILL.md', start_byte: 0, end_byte: 1, message: 'External reference requires review.' }] })
    }
    return sendJson(response, 200, resource(item, requestedRevision, requestedRevision === previousDigest && item.name === 'secure-review' ? { reason: 'superseded policy' } : undefined))
  }
  sendJson(response, 404, { error: 'not found' })
})

server.listen(port, '127.0.0.1', () => {
  process.stdout.write(`Mock Marketplace listening on http://127.0.0.1:${port}\n`)
})
