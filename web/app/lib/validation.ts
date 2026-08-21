import { RESOURCE_KINDS } from '~/types/marketplace'
import type { Marketplace, ResourceKind, ResourceListItem, ResourceRevision, SearchResult, Source, SourceOccurrence, WellKnown } from '~/types/marketplace'

const MAX_STRING_LENGTH = 4096
const MAX_ARRAY_LENGTH = 100
const MAX_OBJECT_KEYS = 100
const DIGEST_PATTERN = /^sha256:[0-9a-f]{64}$/
// Older stores used bounded, opaque digest fixtures such as
// `sha256:manifest123`. Keep accepting that wire form for source history only;
// routeable resource revisions still require canonical 64-character digests.
const LEGACY_HISTORY_DIGEST_PATTERN = /^sha256:[A-Za-z0-9._~-]{1,256}$/

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

function isHistoryDigest(value: string): boolean {
  return LEGACY_HISTORY_DIGEST_PATTERN.test(value)
}

export class ApiValidationError extends Error {
  constructor(message: string) {
    super(`Invalid Marketplace API response: ${message}`)
    this.name = 'ApiValidationError'
  }
}

export function normalizeWellKnown(input: unknown): WellKnown {
  const object = asObject(input, 'metadata')
  return {
    instance_id: requiredString(object.instance_id, 'instance_id'),
    canonical_url: requiredString(object.canonical_url, 'canonical_url'),
    api_base: requiredString(object.api_base, 'api_base'),
    browser_route_template: requiredString(object.browser_route_template, 'browser_route_template'),
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
    origins: stringArray(object.origins, 'origins'),
    ref: requiredString(object.ref, 'ref'),
    roots: boundedArray(object.roots, 'roots').map((root, index) => normalizeSourceRoot(root, `roots[${index}]`)),
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
  const commit = object.commit === undefined ? boundedString(object.revision_commit, `${label}.revision_commit`) : boundedString(object.commit, `${label}.commit`)
  if (object.commit !== undefined && object.revision_commit !== undefined && boundedString(object.revision_commit, `${label}.revision_commit`) !== commit) {
    throw new ApiValidationError(`${label}.commit and revision_commit do not match`)
  }
  return {
    source_id: requiredString(object.source_id, `${label}.source_id`),
    ref: requiredString(object.ref, `${label}.ref`),
    commit: requiredString(commit, `${label}.commit`),
    source_revision_manifest_digest: requiredHistoryDigest(object.source_revision_manifest_digest, `${label}.source_revision_manifest_digest`),
    acquisition: requiredAcquisition(object.acquisition, `${label}.acquisition`),
    ...(object.mirrored_artifact_digest === undefined ? {} : { mirrored_artifact_digest: requiredHistoryDigest(object.mirrored_artifact_digest, `${label}.mirrored_artifact_digest`) }),
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
  const digest = requiredString(object.content_digest, `${label}.content_digest`)
  if (!isSha256Digest(digest)) throw new ApiValidationError(`${label}.content_digest is not a SHA-256 digest`)
  const metadata = object.metadata === undefined ? undefined : stringRecord(object.metadata, `${label}.metadata`)
  return {
    source_id: requiredString(object.source_id, `${label}.source_id`),
    ...(object.source_slug === undefined ? {} : { source_slug: boundedString(object.source_slug, `${label}.source_slug`) }),
    kind,
    name: requiredString(object.name, `${label}.name`),
    revision: requiredString(object.revision, `${label}.revision`),
    content_digest: digest,
    ...(object.description === undefined ? {} : { description: boundedString(object.description, `${label}.description`) }),
    ...(metadata === undefined ? {} : { metadata }),
    ...(object.score === undefined ? {} : { score: boundedNumber(object.score, `${label}.score`) }),
  }
}

export type ExpectedResourceCoordinate = {
  source: string
  kind: ResourceKind
  name: string
  contentDigest?: string
}

export function normalizeResourceRevision(input: unknown, expected?: ExpectedResourceCoordinate): ResourceRevision {
  const object = asObject(input, 'resource revision')
  const coordinate = stringRecord(object.coordinate, 'coordinate')
  const digest = requiredDigest(object.content_digest, 'content_digest')
  if (expected?.contentDigest !== undefined && digest !== expected.contentDigest) {
    throw new ApiValidationError(`content_digest does not match requested digest ${expected.contentDigest}`)
  }
  validateRequestedCoordinate(coordinate, expected)
  return {
    coordinate,
    content_digest: digest,
    ...optionalString(object, 'archive_digest'),
    ...optionalString(object, 'analyzer_version'),
    ...optionalString(object, 'license'),
    ...optionalString(object, 'acquisition'),
    findings: boundedUnknownArray(object.findings, 'findings'),
    provenance: boundedUnknownRecord(object.provenance, 'provenance'),
    revocation: boundedUnknownRecord(object.revocation, 'revocation'),
  }
}

function validateRequestedCoordinate(coordinate: Record<string, string>, expected: ExpectedResourceCoordinate | undefined): void {
  if (!expected) return
  const coordinateSource = coordinate.source ?? coordinate.source_id ?? coordinate.source_slug
  if (coordinateSource !== undefined && coordinateSource !== expected.source) {
    throw new ApiValidationError(`coordinate.source does not match requested source ${expected.source}`)
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

function requiredHistoryDigest(value: unknown, label: string): string {
  const digest = requiredString(value, label)
  if (!isHistoryDigest(digest)) throw new ApiValidationError(`${label} is not a supported bounded digest`)
  return digest
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

function normalizeCapabilities(object: Record<string, unknown>) {
  return {
    api_versions: stringArray(object.api_versions, 'api_versions'),
    artifact_schema_versions: stringArray(object.artifact_schema_versions, 'artifact_schema_versions'),
    resource_kinds: stringArray(object.resource_kinds, 'resource_kinds'),
    acquisition_modes: stringArray(object.acquisition_modes, 'acquisition_modes'),
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
