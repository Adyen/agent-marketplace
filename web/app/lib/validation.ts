import { RESOURCE_KINDS } from '~/types/marketplace'
import type { ResourceKind, ResourceListItem, ResourceRevision, SearchResult, WellKnown } from '~/types/marketplace'

const MAX_STRING_LENGTH = 4096
const MAX_ARRAY_LENGTH = 100
const MAX_OBJECT_KEYS = 100
const DIGEST_PATTERN = /^sha256:[0-9a-f]{64}$/

export function isSha256Digest(value: string): boolean {
  return DIGEST_PATTERN.test(value)
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
  const digest = requiredString(object.content_digest, 'content_digest')
  if (!isSha256Digest(digest)) throw new ApiValidationError('content_digest is not a SHA-256 digest')
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

function boundedArray(value: unknown, label: string): unknown[] {
  if (!Array.isArray(value) || value.length > MAX_ARRAY_LENGTH) throw new ApiValidationError(`${label} must be a bounded array`)
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
