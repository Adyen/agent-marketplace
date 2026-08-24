import { RESOURCE_KINDS } from '~/types/marketplace'
import type { Capabilities, Marketplace, ResourceKind, ResourceListItem, ResourceRevision, SearchResult, Source, SourceOccurrence, WellKnown } from '~/types/marketplace'

const MAX_STRING_LENGTH = 4096
const MAX_ARRAY_LENGTH = 100
const MAX_OBJECT_KEYS = 100
const DIGEST_PATTERN = /^sha256:[0-9a-f]{64}$/
const GIT_COMMIT_PATTERN = /^[0-9a-f]{40}$/

/**
 * Maximum number of source occurrences accepted from one response. The Go
 * endpoint currently returns an array without pagination, so a client-side
 * cap prevents an unbounded history response from consuming the browser.
 * Responses over this limit fail closed and are not partially rendered.
 */
export const MAX_SOURCE_HISTORY_ENTRIES = 1_000

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
  if (browserBase !== undefined && (browserLatestRoute !== `${browserBase}/s/{source}/{kind}/{name}` || browserExactRoute !== `${browserBase}/s/{source}/{kind}/{name}/revisions/{revision}`)) {
    throw new ApiValidationError('browser route templates do not match browser_base')
  }
  return {
    instance_id: requiredString(object.instance_id, 'instance_id'),
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
    canonical_url: requiredString(object.canonical_url, 'canonical_url'),
    ...(object.instance_metadata === undefined ? {} : { instance_metadata: boundedUnknownRecord(object.instance_metadata, 'instance_metadata') }),
  }
}

export function normalizeSource(input: unknown): Source {
  const object = asObject(input, 'source')
  const acquisition = requiredAcquisition(object.acquisition, 'acquisition')
  const aliases = object.aliases === undefined ? undefined : stringArray(object.aliases, 'aliases')
  const publisher = object.publisher === undefined ? undefined : normalizeSourcePublisher(object.publisher)
  return {
    id: requiredString(object.id, 'id'),
    slug: requiredString(object.slug, 'slug'),
    ...(aliases === undefined ? {} : { aliases }),
    origins: requiredStringArray(object.origins, 'origins'),
    ref: requiredString(object.ref, 'ref'),
    roots: requiredArray(object.roots, 'roots').map((root, index) => normalizeSourceRoot(root, `roots[${index}]`)),
    acquisition,
    ...(publisher === undefined ? {} : { publisher }),
    ...(object.license === undefined ? {} : { license: boundedString(object.license, 'license') }),
    listed: requiredBoolean(object.listed, 'listed'),
  }
}

export function normalizeSourceHistory(input: unknown): SourceOccurrence[] {
  return boundedArray(input, 'source history', MAX_SOURCE_HISTORY_ENTRIES).map((item, index) => normalizeSourceOccurrence(item, `source history[${index}]`))
}

export function normalizeSourceOccurrence(input: unknown, label = 'source occurrence'): SourceOccurrence {
  const object = asObject(input, label)
  if (object.revision_commit !== undefined) throw new ApiValidationError(`${label}.revision_commit is unsupported`)
  return {
    source_id: requiredString(object.source_id, `${label}.source_id`),
    ref: requiredString(object.ref, `${label}.ref`),
    commit: requiredCommit(object.commit, `${label}.commit`),
    source_revision_manifest_digest: requiredDigest(object.source_revision_manifest_digest, `${label}.source_revision_manifest_digest`),
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
  return {
    source_id: requiredString(object.source_id, `${label}.source_id`),
    ...(object.source_slug === undefined ? {} : { source_slug: boundedString(object.source_slug, `${label}.source_slug`) }),
    kind,
    name: requiredString(object.name, `${label}.name`),
    revision,
    content_digest: digest,
    ...(object.description === undefined ? {} : { description: boundedString(object.description, `${label}.description`) }),
    ...(metadata === undefined ? {} : { metadata }),
    ...(object.score === undefined ? {} : { score: boundedNumber(object.score, `${label}.score`) }),
  }
}

export type ExpectedResourceCoordinate = {
  // source is the route token and may be a slug or an ID. sourceId is used
  // when the caller has already resolved that token to the canonical ID.
  source?: string
  sourceId?: string
  kind: ResourceKind
  name: string
  contentDigest?: string
}

export function normalizeResourceRevision(input: unknown, expected?: ExpectedResourceCoordinate): ResourceRevision {
  const object = asObject(input, 'resource revision')
  const coordinate = stringRecord(object.coordinate, 'coordinate')
  for (const field of ['marketplace', 'source', 'name']) {
    requiredString(coordinate[field], `coordinate.${field}`)
  }
  const coordinateKind = requiredString(coordinate.kind, 'coordinate.kind')
  if (!isResourceKind(coordinateKind)) throw new ApiValidationError('coordinate.kind is unsupported')
  const digest = requiredDigest(object.content_digest, 'content_digest')
  if (expected?.contentDigest !== undefined && digest !== expected.contentDigest) {
    throw new ApiValidationError(`content_digest does not match requested digest ${expected.contentDigest}`)
  }
  validateRequestedCoordinate(coordinate, expected)
  return {
    coordinate,
    content_digest: digest,
    ...(object.archive_digest === undefined ? {} : { archive_digest: requiredDigest(object.archive_digest, 'archive_digest') }),
    ...optionalString(object, 'analyzer_version'),
    ...optionalString(object, 'license'),
    ...optionalAcquisition(object),
    findings: boundedUnknownArray(object.findings, 'findings'),
    provenance: boundedUnknownRecord(object.provenance, 'provenance'),
    revocation: boundedUnknownRecord(object.revocation, 'revocation'),
  }
}

function validateRequestedCoordinate(coordinate: Record<string, string>, expected: ExpectedResourceCoordinate | undefined): void {
  if (!expected) return
  if (expected.sourceId !== undefined && coordinate.source !== expected.sourceId) {
    throw new ApiValidationError(`coordinate.source does not match requested source ${expected.sourceId}`)
  }
  if (coordinate.kind !== undefined && coordinate.kind !== expected.kind) {
    throw new ApiValidationError(`coordinate.kind does not match requested kind ${expected.kind}`)
  }
  if (coordinate.name !== undefined && coordinate.name !== expected.name) {
    throw new ApiValidationError(`coordinate.name does not match requested name ${expected.name}`)
  }
}

function normalizeSourcePublisher(input: unknown): Source['publisher'] {
  const object = asObject(input, 'publisher')
  return {
    name: boundedString(object.name, 'publisher.name'),
    ...(object.url === undefined ? {} : { url: boundedString(object.url, 'publisher.url') }),
  }
}

function normalizeSourceRoot(input: unknown, label: string): Source['roots'][number] {
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

function requiredAcquisition(value: unknown, label: string): Source['acquisition'] {
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

function optionalAcquisition(object: Record<string, unknown>): Record<string, Source['acquisition']> {
  if (object.acquisition === undefined) return {}
  return { acquisition: requiredAcquisition(object.acquisition, 'acquisition') }
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

function boundedUnknownArray(value: unknown, label: string): unknown[] | undefined {
  if (value === undefined) return undefined
  return boundedArray(value, label).map((item, index) => boundedUnknown(item, `${label}[${index}]`))
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
