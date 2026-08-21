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
const requiredResponseFields = {
  Capabilities: ['api_versions', 'artifact_schema_versions', 'resource_kinds', 'acquisition_modes', 'features'],
  WellKnown: ['instance_id', 'canonical_url', 'api_base', 'browser_route_template'],
  SearchResult: ['items', 'total', 'offset', 'limit'],
  ResourceListItem: ['source_id', 'kind', 'name', 'revision', 'content_digest'],
  ResourceRevision: ['coordinate', 'content_digest'],
}
const expectedGenerator = 'openapi-typescript@7.9.1'

const openapi = (await readFile(openapiPath, 'utf8')).replaceAll('\r\n', '\n')
const contract = (await readFile(contractPath, 'utf8')).replaceAll('\r\n', '\n')
const missingPaths = requiredPaths.filter(path => !openapi.includes(path))
const missingKinds = requiredKinds.filter(kind => {
  const yamlKind = openapi.split('\n').some(line => line.trim() === `- ${kind}`)
  const frontendKind = contract.includes(`'${kind}'`) || contract.includes(`"${kind}"`)
  return !yamlKind || !frontendKind
})
const missingResponseFields = Object.entries(requiredResponseFields).flatMap(([schema, fields]) => {
  const openapiRequired = requiredFieldsInSchema(openapi, schema)
  const frontendRequired = requiredFieldsInInterface(contract, schema)
  return fields
    .filter(field => !openapiRequired.has(field) || !frontendRequired.has(field))
    .map(field => `${schema}.${field}`)
})

if (missingPaths.length || missingKinds.length || missingResponseFields.length) {
  console.error('OpenAPI contract drift detected.')
  if (missingPaths.length) console.error(`Missing API paths: ${missingPaths.join(', ')}`)
  if (missingKinds.length) console.error(`Missing resource kinds: ${missingKinds.join(', ')}`)
  if (missingResponseFields.length) console.error(`Missing required response fields: ${missingResponseFields.join(', ')}`)
  process.exit(1)
}

if (process.argv.includes('--check')) {
  console.log('OpenAPI structural contract check passed: paths, resource kinds, and frontend-required response fields are aligned.')
  process.exit(0)
}

// This command intentionally does not generate types. Keep the hand-authored contract until
// the approved generator can be installed without bypassing pnpm's supply-chain policy.
console.error([
  'OpenAPI TypeScript generation preflight failed.',
  `The web contract remains hand-authored at ${contractPath}; no types were generated.`,
  `A real pinned generator, ${expectedGenerator}, cannot currently be installed under the active pnpm policy.`,
  'The attempted install did not change pnpm security settings:',
  'CI=true pnpm install --frozen-lockfile',
  'pnpm rejected the existing lockfile before the generator could be added:',
  'MINIMUM_RELEASE_AGE_VIOLATION (minimumReleaseAge=30240) and TRUST_DOWNGRADE (semver@6.3.1).',
  'Do not bypass these checks or lower minimumReleaseAge. Add the exact generator only after the lockfile passes the configured policy.',
].join('\n'))
process.exit(1)

function requiredFieldsInSchema(document, schema) {
  const lines = sectionFor(document, `    ${schema}:`).split('\n')
  const requiredIndex = lines.findIndex(line => line.trim() === 'required:')
  if (requiredIndex === -1) return new Set()

  const fields = []
  for (const line of lines.slice(requiredIndex + 1)) {
    const match = line.match(/^\s+- ([^\s#]+)\s*$/)
    if (!match) break
    fields.push(match[1])
  }
  return new Set(fields)
}

function requiredFieldsInInterface(document, interfaceName) {
  const start = document.indexOf(`export interface ${interfaceName} `)
  if (start === -1) return new Set()
  const end = document.indexOf('\n}', start)
  const section = document.slice(start, end === -1 ? document.length : end)
  return new Set([...section.matchAll(/^\s{2}([A-Za-z_][A-Za-z0-9_]*):/gm)].map(([, field]) => field))
}

function sectionFor(document, heading) {
  const lines = document.split('\n')
  const start = lines.indexOf(heading)
  if (start === -1) return ''
  const end = lines.findIndex((line, index) => index > start && /^\s{4}[A-Za-z][A-Za-z0-9]*:$/.test(line))
  return lines.slice(start, end === -1 ? lines.length : end).join('\n')
}
