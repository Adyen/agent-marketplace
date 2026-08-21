import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const webRoot = new URL('../', import.meta.url).pathname
const openapiPath = resolve(webRoot, '..', 'api', 'openapi.yaml')
const contractPath = resolve(webRoot, 'app', 'types', 'marketplace.ts')
const requiredPaths = [
  '/.well-known/agent-marketplace.json:',
  '/api/v1/search:',
  '/api/v1/resources/{source}/{kind}/{name}:',
]
const requiredKinds = ['skill', 'command', 'subagent', 'hook', 'mcpserver', 'collection']

const openapi = await readFile(openapiPath, 'utf8')
const contract = await readFile(contractPath, 'utf8')
const missingPaths = requiredPaths.filter(path => !openapi.includes(path))
const missingKinds = requiredKinds.filter(kind => !contract.includes(`'${kind}'`))

if (missingPaths.length || missingKinds.length) {
  console.error('OpenAPI contract drift detected.')
  if (missingPaths.length) console.error(`Missing API paths: ${missingPaths.join(', ')}`)
  if (missingKinds.length) console.error(`Missing contract kinds: ${missingKinds.join(', ')}`)
  process.exit(1)
}

if (process.argv.includes('--check')) {
  console.log('OpenAPI contract paths and resource kinds match the hand-authored frontend contract types.')
} else {
  console.log('No local OpenAPI generator is pinned; the typed client is intentionally hand-authored from api/openapi.yaml.')
}
