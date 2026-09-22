import { RESOURCE_KINDS } from '~/types/marketplace'
import type { Capabilities, InspectionFile, InspectionManifest, Marketplace, ResourceKind, ResourceListItem, ResourceRevision, SearchResult, Registry, RegistryOccurrence, RegistrySummary, WellKnown } from '~/types/marketplace'

const MAX_STRING_LENGTH = 4096
const MAX_ARRAY_LENGTH = 100
const MAX_REGISTRY_SUMMARIES = 1_000
const MAX_OBJECT_KEYS = 100
const DIGEST_PATTERN = /^sha256:[0-9a-f]{64}$/
const GIT_COMMIT_PATTERN = /^[0-9a-f]{40}$/

/**
 * Maximum number of registry occurrences accepted from one response. The Go
 * endpoint currently returns an array without pagination, so a client-side
 * cap prevents an unbounded history response from consuming the browser.
 * Responses over this limit fail closed and are not partially rendered.
 */
export const MAX_REGISTRY_HISTORY_ENTRIES = 1_000

// Revision history is currently returned as one array by the API. Fail closed
// on unexpectedly large responses so a malicious or misconfigured registry
// cannot make the browser render an unbounded history.
export const MAX_RESOURCE_REVISION_ENTRIES = 100

// The analyzer emits at most 1,000 findings by default. Keep that complete
// security record renderable while still bounding an untrusted API response.
export const MAX_ASSESSMENT_FINDINGS = 1_000

export function isSha256Digest(value: string): boolean {
  return DIGEST_PATTERN.test(value)
}

function isCanonicalGitCommit(value: string): boolean {
  return GIT_COMMIT_PATTERN.test(value)
}

export class ApiValidationError extends Error {
  constructor(message: string) {
    super(`Invalid Marketplace API response: ${message}`)
    this.name = 'ApiValidationError'
  }
}

export function normalizeWellKnown(input: unknown): WellKnown {
  const object = asObject(input, 'metadata')
  const browserBase = object.browser_base === undefined ? undefined : requiredString(object.browser_base, 'browser_base')
  const browserLatestRoute = object.browser_latest_route_template === undefined ? undefined : requiredString(object.browser_latest_route_template, 'browser_latest_route_template')
  const browserExactRoute = object.browser_exact_route_template === undefined ? undefined : requiredString(object.browser_exact_route_template, 'browser_exact_route_template')
  const browserFields = [browserBase, browserLatestRoute, browserExactRoute]
  if (browserFields.some(value => value !== undefined) && browserFields.some(value => value === undefined)) {
    throw new ApiValidationError('browser fields must be provided together')
  }
  if (browserBase !== undefined && (browserLatestRoute !== `${browserBase}/r/{registry}/{kind}/{name}` || browserExactRoute !== `${browserBase}/r/{registry}/{kind}/{name}/revisions/{revision}`)) {
    throw new ApiValidationError('browser route templates do not match browser_base')
  }
  return {
    instance_id: requiredString(object.instance_id, 'instance_id'),
    version: requiredString(object.version, 'version'),
    canonical_url: requiredString(object.canonical_url, 'canonical_url'),
    api_base: requiredString(object.api_base, 'api_base'),
    ...(browserBase === undefined ? {} : { browser_base: browserBase, browser_latest_route_template: browserLatestRoute, browser_exact_route_template: browserExactRoute }),
    ...normalizeCapabilities(object),
  }
}

export function normalizeMarketplace(input: unknown): Marketplace {
  const object = asObject(input, 'marketplace')
  return {
    id: requiredString(object.id, 'id'),
    name: requiredString(object.name, 'name'),
    version: requiredString(object.version, 'version'),
    canonical_url: requiredString(object.canonical_url, 'canonical_url'),
    ...(object.instance_metadata === undefined ? {} : { instance_metadata: boundedUnknownRecord(object.instance_metadata, 'instance_metadata') }),
  }
}

export function normalizeRegistrySummaries(input: unknown): RegistrySummary[] {
  return boundedArray(input, 'registries', MAX_REGISTRY_SUMMARIES).map((item, index) => {
    const object = asObject(item, `registries[${index}]`)
    return {
      id: requiredString(object.id, `registries[${index}].id`),
      slug: requiredString(object.slug, `registries[${index}].slug`),
    }
  })
}

export function normalizeRegistry(input: unknown): Registry {
  const object = asObject(input, 'registry')
  const acquisition = requiredAcquisition(object.acquisition, 'acquisition')
  const aliases = object.aliases === undefined ? undefined : stringArray(object.aliases, 'aliases')
  const publisher = object.publisher === undefined ? undefined : normalizeRegistryPublisher(object.publisher)
  return {
    id: requiredString(object.id, 'id'),
    slug: requiredString(object.slug, 'slug'),
    ...(aliases === undefined ? {} : { aliases }),
    origins: requiredStringArray(object.origins, 'origins'),
    ref: requiredString(object.ref, 'ref'),
    roots: requiredArray(object.roots, 'roots').map((root, index) => normalizeRegistryRoot(root, `roots[${index}]`)),
    acquisition,
    ...(publisher === undefined ? {} : { publisher }),
    ...(object.license === undefined ? {} : { license: boundedString(object.license, 'license') }),
    listed: requiredBoolean(object.listed, 'listed'),
  }
}

export function normalizeRegistryHistory(input: unknown): RegistryOccurrence[] {
  return boundedArray(input, 'registry history', MAX_REGISTRY_HISTORY_ENTRIES).map((item, index) => normalizeRegistryOccurrence(item, `registry history[${index}]`))
}

export function normalizeResourceRevisions(input: unknown, expected: ExpectedResourceCoordinate): ResourceRevision[] {
  return boundedArray(input, 'resource revisions', MAX_RESOURCE_REVISION_ENTRIES).map(item => normalizeResourceRevisionValue(item, { kind: expected.kind, name: expected.name }, false))
}

export function normalizeRegistryOccurrence(input: unknown, label = 'registry occurrence'): RegistryOccurrence {
  const object = asObject(input, label)
  if (object.revision_commit !== undefined) throw new ApiValidationError(`${label}.revision_commit is unsupported`)
  return {
    registry_id: requiredString(object.registry_id, `${label}.registry_id`),
    ref: requiredString(object.ref, `${label}.ref`),
    roots: requiredArray(object.roots, `${label}.roots`).map((root, index) => normalizeRegistryRoot(root, `${label}.roots[${index}]`)),
    commit: requiredCommit(object.commit, `${label}.commit`),
    registry_revision_manifest_digest: requiredDigest(object.registry_revision_manifest_digest, `${label}.registry_revision_manifest_digest`),
    acquisition: requiredAcquisition(object.acquisition, `${label}.acquisition`),
    ...(object.mirrored_artifact_digest === undefined ? {} : { mirrored_artifact_digest: requiredDigest(object.mirrored_artifact_digest, `${label}.mirrored_artifact_digest`) }),
    occurred_at: dateTimeString(object.occurred_at, `${label}.occurred_at`),
  }
}

export function normalizeSearchResult(input: unknown): SearchResult {
  const object = asObject(input, 'search result')
  const rawItems = boundedArray(object.items, 'items')
  return {
    items: rawItems.map((item, index) => normalizeResourceListItem(item, `items[${index}]`)),
    total: boundedInteger(object.total, 'total'),
    offset: boundedInteger(object.offset, 'offset'),
    limit: boundedInteger(object.limit, 'limit'),
  }
}

export function normalizeResourceListItem(input: unknown, label = 'resource'): ResourceListItem {
  const object = asObject(input, label)
  const kind = requiredString(object.kind, `${label}.kind`)
  if (!isResourceKind(kind)) throw new ApiValidationError(`${label}.kind is unsupported`)
  const digest = requiredDigest(object.content_digest, `${label}.content_digest`)
  const revision = requiredDigest(object.revision, `${label}.revision`)
  if (revision !== digest) throw new ApiValidationError(`${label}.revision and content_digest do not match`)
  const metadata = object.metadata === undefined ? undefined : stringRecord(object.metadata, `${label}.metadata`)
  const availability = normalizeListAvailability(object.availability, `${label}.availability`)
  return {
    registry_id: requiredString(object.registry_id, `${label}.registry_id`),
    ...(object.registry_slug === undefined ? {} : { registry_slug: boundedString(object.registry_slug, `${label}.registry_slug`) }),
    kind,
    name: requiredString(object.name, `${label}.name`),
    revision,
    content_digest: digest,
    ...(object.description === undefined ? {} : { description: boundedString(object.description, `${label}.description`) }),
    ...(metadata === undefined ? {} : { metadata }),
    ...(object.score === undefined ? {} : { score: boundedNumber(object.score, `${label}.score`) }),
    ...(availability === undefined ? {} : { availability }),
  }
}

const LIST_AVAILABILITIES = ['available', 'blocked', 'policy-blocked', 'operator-exposed', 'no-artifact', 'revoked'] as const

/**
 * Keep only an availability value this client understands. A value a newer
 * server adds is dropped rather than rejected, so the caller treats it as
 * unknown instead of failing the whole response. A non-string is malformed
 * and still throws.
 */
function normalizeListAvailability(value: unknown, label: string): ResourceListItem['availability'] {
  if (value === undefined) return undefined
  const text = requiredString(value, label)
  return (LIST_AVAILABILITIES as readonly string[]).includes(text)
    ? (text as NonNullable<ResourceListItem['availability']>)
    : undefined
}

export type ExpectedResourceCoordinate = {
  // registry is the route token and may be a slug or an ID. registryId is used
  // when the caller has already resolved that token to the canonical ID.
  registry?: string
  registryId?: string
  kind: ResourceKind
  name: string
  contentDigest?: string
}

export function normalizeResourceRevision(input: unknown, expected?: ExpectedResourceCoordinate): ResourceRevision {
  return normalizeResourceRevisionValue(input, expected, true)
}

function normalizeResourceRevisionValue(input: unknown, expected: ExpectedResourceCoordinate | undefined, includeFindings: boolean): ResourceRevision {
  const object = asObject(input, 'resource revision')
  for (const legacyField of ['analyzer_version', 'findings', 'provenance', 'acquisition']) {
    if (object[legacyField] !== undefined) throw new ApiValidationError(`${legacyField} is unsupported; use assessment or occurrence`)
  }
  const coordinate = normalizeResourceCoordinate(object.coordinate, 'coordinate')
  const digest = requiredDigest(object.content_digest, 'content_digest')
  if (expected?.contentDigest !== undefined && digest !== expected.contentDigest) {
    throw new ApiValidationError(`content_digest does not match requested digest ${expected.contentDigest}`)
  }
  validateRequestedCoordinate(coordinate, expected)

  const assessmentObject = asObject(object.assessment, 'assessment')
  const assessmentCoordinate = normalizeMatchingCoordinate(assessmentObject.coordinate, coordinate, 'assessment.coordinate')
  const assessmentContentDigest = matchingDigest(assessmentObject.content_digest, digest, 'assessment.content_digest')
  const decision = requiredString(assessmentObject.decision, 'assessment.decision')
  if (!['allow', 'warn', 'reject'].includes(decision)) throw new ApiValidationError('assessment.decision is unsupported')
  const findingValues = assessmentObject.findings === undefined ? undefined : boundedArray(assessmentObject.findings, 'assessment.findings', MAX_ASSESSMENT_FINDINGS)
  const findings = !includeFindings || findingValues === undefined ? undefined : findingValues.map((value, index) => {
    const finding = asObject(value, `assessment.findings[${index}]`)
    const locationObject = finding.location === undefined ? undefined : asObject(finding.location, `assessment.findings[${index}].location`)
    const startByte = locationObject?.start_byte === undefined ? undefined : boundedInteger(locationObject.start_byte, `assessment.findings[${index}].location.start_byte`)
    const endByte = locationObject?.end_byte === undefined ? undefined : boundedInteger(locationObject.end_byte, `assessment.findings[${index}].location.end_byte`)
    if (startByte !== undefined && endByte !== undefined && startByte > endByte) throw new ApiValidationError(`assessment.findings[${index}].location has an inverted byte range`)
    const location = locationObject === undefined ? undefined : {
      path: requiredString(locationObject.path, `assessment.findings[${index}].location.path`),
      ...(locationObject.line === undefined ? {} : { line: boundedInteger(locationObject.line, `assessment.findings[${index}].location.line`) }),
      ...(locationObject.column === undefined ? {} : { column: boundedInteger(locationObject.column, `assessment.findings[${index}].location.column`) }),
      ...(startByte === undefined ? {} : { start_byte: startByte }),
      ...(endByte === undefined ? {} : { end_byte: endByte }),
    }
    return {
      code: requiredString(finding.code, `assessment.findings[${index}].code`),
      severity: requiredString(finding.severity, `assessment.findings[${index}].severity`),
      ...optionalString(finding, 'policy'),
      ...optionalString(finding, 'message'),
      ...(location === undefined ? {} : { location }),
    }
  })
  const assessment: ResourceRevision['assessment'] = {
    coordinate: assessmentCoordinate,
    content_digest: assessmentContentDigest,
    assessment_digest: requiredDigest(assessmentObject.assessment_digest, 'assessment.assessment_digest'),
    analyzer_version: requiredString(assessmentObject.analyzer_version, 'assessment.analyzer_version'),
    policy_digest: requiredDigest(assessmentObject.policy_digest, 'assessment.policy_digest'),
    decision: decision as ResourceRevision['assessment']['decision'],
    ...(findings === undefined ? {} : { findings }),
    inspection_manifest_digest: requiredDigest(assessmentObject.inspection_manifest_digest, 'assessment.inspection_manifest_digest'),
    assessed_at: dateTimeString(assessmentObject.assessed_at, 'assessment.assessed_at'),
  }

  const occurrenceObject = asObject(object.occurrence, 'occurrence')
  const occurrence: ResourceRevision['occurrence'] = {
    coordinate: normalizeMatchingCoordinate(occurrenceObject.coordinate, coordinate, 'occurrence.coordinate'),
    content_digest: matchingDigest(occurrenceObject.content_digest, digest, 'occurrence.content_digest'),
    commit: requiredCommit(occurrenceObject.commit, 'occurrence.commit'),
    manifest_digest: requiredDigest(occurrenceObject.manifest_digest, 'occurrence.manifest_digest'),
  }

  const currentAvailabilityObject = object.current_availability === undefined ? undefined : asObject(object.current_availability, 'current_availability')
  const currentAvailability = currentAvailabilityObject === undefined ? undefined : normalizeListAvailability(currentAvailabilityObject.availability, 'current_availability.availability')
  const revocationObject = object.revocation === undefined ? undefined : asObject(object.revocation, 'revocation')
  const revocationKind = revocationObject === undefined ? undefined : requiredString(revocationObject.kind, 'revocation.kind')
  if (revocationKind !== undefined && !['registry', 'coordinate', 'revision'].includes(revocationKind)) throw new ApiValidationError('revocation.kind is unsupported')
  return {
    coordinate,
    content_digest: digest,
    ...(object.archive_digest === undefined ? {} : { archive_digest: requiredDigest(object.archive_digest, 'archive_digest') }),
    ...optionalString(object, 'license'),
    ...optionalString(object, 'description'),
    assessment,
    occurrence,
    ...(currentAvailability === undefined ? {} : { current_availability: { availability: currentAvailability } }),
    ...(revocationObject === undefined ? {} : { revocation: {
      kind: revocationKind as NonNullable<ResourceRevision['revocation']>['kind'],
      target_id: requiredString(revocationObject.target_id, 'revocation.target_id'),
      reason: requiredString(revocationObject.reason, 'revocation.reason'),
      applied_at: dateTimeString(revocationObject.applied_at, 'revocation.applied_at'),
    } }),
  }
}

function normalizeResourceCoordinate(input: unknown, label: string): ResourceRevision['coordinate'] {
  const coordinate = stringRecord(input, label)
  for (const field of ['marketplace', 'registry', 'name']) requiredString(coordinate[field], `${label}.${field}`)
  const kind = requiredString(coordinate.kind, `${label}.kind`)
  if (!isResourceKind(kind)) throw new ApiValidationError(`${label}.kind is unsupported`)
  return { marketplace: coordinate.marketplace!, registry: coordinate.registry!, kind, name: coordinate.name! }
}

function normalizeMatchingCoordinate(input: unknown, expected: ResourceRevision['coordinate'], label: string): ResourceRevision['coordinate'] {
  const coordinate = normalizeResourceCoordinate(input, label)
  for (const field of ['marketplace', 'registry', 'kind', 'name'] as const) {
    if (coordinate[field] !== expected[field]) throw new ApiValidationError(`${label}.${field} does not match resource coordinate`)
  }
  return coordinate
}

function matchingDigest(input: unknown, expected: string, label: string): string {
  const digest = requiredDigest(input, label)
  if (digest !== expected) throw new ApiValidationError(`${label} does not match content_digest`)
  return digest
}

export function normalizeInspectionManifest(input: unknown, expected?: ExpectedResourceCoordinate): InspectionManifest {
  const object = asObject(input, 'inspection manifest')
  if (object.findings !== undefined) throw new ApiValidationError('inspection manifest findings are unsupported; use assessment findings')
  if (requiredString(object.schema_version, 'schema_version') !== 'v1') throw new ApiValidationError('unsupported inspection schema')
  const coordinate = stringRecord(object.coordinate, 'coordinate') as InspectionManifest['coordinate']
  for (const [field, value] of Object.entries(coordinate)) requiredString(value, `coordinate.${field}`)
  const coordinateKind = requiredString(coordinate.kind, 'coordinate.kind')
  if (!isResourceKind(coordinateKind)) throw new ApiValidationError('inspection kind is unsupported')
  const revision = requiredDigest(object.revision_digest, 'revision_digest')
  if (expected?.contentDigest !== undefined && revision !== expected.contentDigest) throw new ApiValidationError('inspection revision does not match request')
  validateRequestedCoordinate(coordinate, expected)
  const primaryPath = requiredString(object.primary_path, 'primary_path')
  if (primaryPath !== inspectionPrimaryPath(coordinateKind)) throw new ApiValidationError('inspection primary path is invalid')
  const seenPaths = new Set<string>()
  const files = boundedArray(object.files, 'files').map((value, index) => {
    const file = asObject(value, `files[${index}]`)
    const size = boundedInteger(file.size, `files[${index}].size`)
    if (size < 0) throw new ApiValidationError(`files[${index}].size is negative`)
    const inspectionClass = requiredString(file.inspection_class, `files[${index}].inspection_class`)
    if (!['text', 'image', 'metadata'].includes(inspectionClass)) throw new ApiValidationError(`files[${index}].inspection_class is invalid`)
    const mediaType = requiredString(file.media_type, `files[${index}].media_type`)
    const mediaClass = mediaTypeClass(mediaType)
    if (mediaClass === undefined || mediaClass !== inspectionClass) throw new ApiValidationError(`files[${index}] media type and inspection class disagree`)
    const filePath = requiredString(file.path, `files[${index}].path`)
    if (filePath === primaryPath) {
      const [expectedMedia, expectedClass] = inspectionPrimaryMediaPolicy(coordinateKind)
      if (mediaType !== expectedMedia || inspectionClass !== expectedClass) throw new ApiValidationError(`files[${index}] primary media policy is not canonical`)
    }
    if (seenPaths.has(filePath) || (filePath !== primaryPath && !canonicalSupportPath(filePath))) throw new ApiValidationError(`files[${index}].path is not canonical`)
    seenPaths.add(filePath)
    return {
      path: filePath,
      content_digest: requiredDigest(file.content_digest, `files[${index}].content_digest`),
      media_type: mediaType as InspectionFile['media_type'],
      size,
      inspection_class: inspectionClass as InspectionFile['inspection_class'],
    }
  })
  if (!seenPaths.has(primaryPath)) throw new ApiValidationError('inspection primary file is missing')
  return { schema_version: 'v1', coordinate, revision_digest: revision, primary_path: primaryPath, files }
}

function inspectionPrimaryPath(kind: ResourceKind): InspectionManifest['primary_path'] {
  switch (kind) {
    case 'skill': return 'SKILL.md'
    case 'command': return 'COMMAND.md'
    case 'subagent': return 'AGENT.md'
    case 'hook': return 'HOOK.md'
    case 'mcpserver': return '.mcp.json'
    case 'collection': return 'COLLECTION.json'
  }
}

function inspectionPrimaryMediaPolicy(kind: ResourceKind): [string, InspectionFile['inspection_class']] {
  switch (kind) {
    case 'skill':
    case 'command':
    case 'subagent':
    case 'hook':
      return ['text/markdown', 'text']
    case 'mcpserver':
    case 'collection':
      return ['application/json', 'text']
  }
}

function canonicalSupportPath(value: string): boolean {
  if (!value.startsWith('support/') || value === 'support/' || value.includes('\\')) return false
  const name = value.slice('support/'.length)
  if (['SKILL.md', 'COMMAND.md', 'AGENT.md', 'HOOK.md', '.mcp.json', 'COLLECTION.json'].includes(name)) return false
  return name.split('/').every(part => part !== '' && part !== '.' && part !== '..')
}

function mediaTypeClass(mediaType: string): InspectionFile['inspection_class'] | undefined {
  if (['text/markdown', 'text/plain', 'application/json', 'application/yaml'].includes(mediaType)) return 'text'
  if (['image/png', 'image/jpeg', 'image/gif', 'image/webp'].includes(mediaType)) return 'image'
  if (mediaType === 'application/octet-stream') return 'metadata'
  return undefined
}

function validateRequestedCoordinate(coordinate: { registry?: string; kind?: string; name?: string }, expected: ExpectedResourceCoordinate | undefined): void {
  if (!expected) return
  if (expected.registryId !== undefined && coordinate.registry !== expected.registryId) {
    throw new ApiValidationError(`coordinate.registry does not match requested registry ${expected.registryId}`)
  }
  if (coordinate.kind !== undefined && coordinate.kind !== expected.kind) {
    throw new ApiValidationError(`coordinate.kind does not match requested kind ${expected.kind}`)
  }
  if (coordinate.name !== undefined && coordinate.name !== expected.name) {
    throw new ApiValidationError(`coordinate.name does not match requested name ${expected.name}`)
  }
}

function normalizeRegistryPublisher(input: unknown): Registry['publisher'] {
  const object = asObject(input, 'publisher')
  return {
    name: boundedString(object.name, 'publisher.name'),
    ...(object.url === undefined ? {} : { url: boundedString(object.url, 'publisher.url') }),
  }
}

function normalizeRegistryRoot(input: unknown, label: string): Registry['roots'][number] {
  const object = asObject(input, label)
  return {
    path: requiredString(object.path, `${label}.path`),
    ...(object.source_harness === undefined ? {} : { source_harness: boundedString(object.source_harness, `${label}.source_harness`) }),
  }
}

function requiredDigest(value: unknown, label: string): string {
  const digest = requiredString(value, label)
  if (!isSha256Digest(digest)) throw new ApiValidationError(`${label} is not a SHA-256 digest`)
  return digest
}

function requiredCommit(value: unknown, label: string): string {
  const commit = requiredString(value, label)
  if (!isCanonicalGitCommit(commit)) throw new ApiValidationError(`${label} is not a canonical Git commit`)
  return commit
}

function requiredAcquisition(value: unknown, label: string): Registry['acquisition'] {
  const acquisition = requiredString(value, label)
  if (acquisition !== 'index-only' && acquisition !== 'mirror') throw new ApiValidationError(`${label} is unsupported`)
  return acquisition
}

function requiredBoolean(value: unknown, label: string): boolean {
  if (typeof value !== 'boolean') throw new ApiValidationError(`${label} must be a boolean`)
  return value
}

function dateTimeString(value: unknown, label: string): string {
  const result = requiredString(value, label)
  if (!Number.isFinite(Date.parse(result))) throw new ApiValidationError(`${label} must be a date-time`)
  return result
}

function normalizeCapabilities(object: Record<string, unknown>): Capabilities {
  return {
    api_versions: requiredEnumArray(object.api_versions, 'api_versions', ['v1'] as const),
    artifact_schema_versions: requiredEnumArray(object.artifact_schema_versions, 'artifact_schema_versions', ['v1'] as const),
    resource_kinds: requiredEnumArray(object.resource_kinds, 'resource_kinds', RESOURCE_KINDS),
    acquisition_modes: requiredEnumArray(object.acquisition_modes, 'acquisition_modes', ['index-only', 'mirror'] as const),
    features: stringArray(object.features, 'features'),
  }
}

function asObject(value: unknown, label: string): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new ApiValidationError(`${label} must be an object`)
  const entries = Object.entries(value)
  if (entries.length > MAX_OBJECT_KEYS) throw new ApiValidationError(`${label} has too many fields`)
  return value as Record<string, unknown>
}

function requiredString(value: unknown, label: string): string {
  const result = boundedString(value, label)
  if (!result.trim()) throw new ApiValidationError(`${label} must not be empty`)
  return result
}

function boundedString(value: unknown, label: string): string {
  if (typeof value !== 'string' || value.length > MAX_STRING_LENGTH) throw new ApiValidationError(`${label} must be a bounded string`)
  return value
}

function optionalString(object: Record<string, unknown>, key: string): Record<string, string> {
  return object[key] === undefined ? {} : { [key]: boundedString(object[key], key) }
}

function stringArray(value: unknown, label: string): string[] {
  return boundedArray(value, label).map((item, index) => boundedString(item, `${label}[${index}]`))
}

function requiredStringArray(value: unknown, label: string): string[] {
  const values = stringArray(value, label)
  if (values.length === 0) throw new ApiValidationError(`${label} must not be empty`)
  return values
}

function requiredArray(value: unknown, label: string): unknown[] {
  const values = boundedArray(value, label)
  if (values.length === 0) throw new ApiValidationError(`${label} must not be empty`)
  return values
}

function enumArray<const Value extends string>(value: unknown, label: string, allowed: readonly Value[]): Value[] {
  return stringArray(value, label).map((item, index) => {
    if (!(allowed as readonly string[]).includes(item)) throw new ApiValidationError(`${label}[${index}] is unsupported`)
    return item as Value
  })
}

function requiredEnumArray<const Value extends string>(value: unknown, label: string, allowed: readonly Value[]): Value[] {
  const values = enumArray(value, label, allowed)
  if (values.length === 0) throw new ApiValidationError(`${label} must not be empty`)
  return values
}

function boundedArray(value: unknown, label: string, maxLength = MAX_ARRAY_LENGTH): unknown[] {
  if (!Array.isArray(value) || value.length > maxLength) throw new ApiValidationError(`${label} must be a bounded array of at most ${maxLength} entries`)
  return value
}

function stringRecord(value: unknown, label: string): Record<string, string> {
  const object = asObject(value, label)
  return Object.fromEntries(Object.entries(object).map(([key, item]) => [boundedString(key, `${label} key`), boundedString(item, `${label}.${key}`)]))
}

function boundedUnknownRecord(value: unknown, label: string): Record<string, unknown> | undefined {
  if (value === undefined) return undefined
  const object = asObject(value, label)
  return Object.fromEntries(Object.entries(object).map(([key, item]) => [boundedString(key, `${label} key`), boundedUnknown(item, `${label}.${key}`)]))
}

function boundedUnknown(value: unknown, label: string, depth = 0): unknown {
  if (depth > 4) throw new ApiValidationError(`${label} is too deeply nested`)
  if (value === null || typeof value === 'boolean') return value
  if (typeof value === 'string') return boundedString(value, label)
  if (typeof value === 'number') return boundedNumber(value, label)
  if (Array.isArray(value)) return boundedArray(value, label).map((item, index) => boundedUnknown(item, `${label}[${index}]`, depth + 1))
  const object = asObject(value, label)
  return Object.fromEntries(Object.entries(object).map(([key, item]) => [boundedString(key, `${label} key`), boundedUnknown(item, `${label}.${key}`, depth + 1)]))
}

function boundedInteger(value: unknown, label: string): number {
  if (!Number.isInteger(value) || (value as number) < 0 || (value as number) > 1_000_000_000) throw new ApiValidationError(`${label} must be a bounded non-negative integer`)
  return value as number
}

function boundedNumber(value: unknown, label: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value) || Math.abs(value) > 1_000_000_000) throw new ApiValidationError(`${label} must be a bounded number`)
  return value
}

function isResourceKind(value: string): value is ResourceKind {
  return (RESOURCE_KINDS as readonly string[]).includes(value)
}
