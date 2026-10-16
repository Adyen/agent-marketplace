import type { ResourceKind, ResourceListItem, ResourceRevision, ResourceSource } from '~/types/marketplace'
import { isSafeRouteToken, isSafeResourceRoute } from '~/lib/routes'
import { isSha256Digest } from '~/lib/validation'

export const KIND_LABELS: Record<ResourceKind, string> = {
  skill: 'Skill',
  command: 'Command',
  subagent: 'Subagent',
  hook: 'Hook',
  mcpserver: 'MCP server',
  collection: 'Collection',
}

export const KIND_ICONS: Record<ResourceKind, string> = {
  skill: 'i-lucide-sparkles',
  command: 'i-lucide-terminal',
  subagent: 'i-lucide-bot',
  hook: 'i-lucide-webhook',
  mcpserver: 'i-lucide-plug',
  collection: 'i-lucide-library',
}

export function kindLabel(kind: ResourceKind): string {
  return KIND_LABELS[kind]
}

export function kindIcon(kind: ResourceKind): string {
  return KIND_ICONS[kind]
}

export function registryLabel(item: ResourceListItem): string {
  return item.registry_slug || item.registry_id
}

export function safeCanonicalUrl(value: string | undefined): string {
  if (!value || hasUnsafeURLCharacters(value) || /%(?:0[0-9a-f]|1[0-9a-f]|7f)/i.test(value)) return ''
  try {
    const url = new URL(value)
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || !url.hostname || url.search || url.hash) return ''
    return `${url.origin}${url.pathname.replace(/\/+$/, '')}`
  } catch {
    return ''
  }
}

export function registryRoute(item: Pick<ResourceListItem, 'registry_id' | 'registry_slug'>): string {
  const registry = item.registry_slug || item.registry_id
  if (!isSafeRouteToken(item.registry_id) || (item.registry_slug !== undefined && !isSafeRouteToken(item.registry_slug))) return ''
  return `/registries/${encodeURIComponent(registry)}`
}

export function resourceLatestRoute(item: ResourceListItem): string {
  const registry = item.registry_slug || item.registry_id
  if (!registryRoute(item) || !isSafeResourceRoute(registry, item.kind, item.name)) return ''
  return `/r/${encodeURIComponent(registry)}/${encodeURIComponent(item.kind)}/${encodeURIComponent(item.name)}`
}

export function resourceLatestLocator(item: ResourceListItem, browserBase: string): string {
  const canonical = safeCanonicalUrl(browserBase)
  const route = resourceLatestRoute(item)
  return canonical && route ? `${canonical}${route}` : ''
}

export function resourceRoute(item: ResourceListItem): string {
  const latest = resourceLatestRoute(item)
  if (!latest || !isSha256Digest(item.content_digest)) return ''
  return `${latest}/revisions/${encodeURIComponent(item.content_digest)}`
}

export function resourceLocator(item: ResourceListItem, browserBase: string): string {
  const canonical = safeCanonicalUrl(browserBase)
  const route = resourceRoute(item)
  return canonical && route ? `${canonical}${route}` : ''
}

export function installCommand(item: ResourceListItem, browserBase: string): string {
  const locator = resourceLocator(item, browserBase)
  return locator ? `agent-marketplace install "${locator}"` : ''
}

function parseHTTPURL(value: string): URL | undefined {
  try {
    const url = new URL(value)
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password && url.hostname ? url : undefined
  } catch {
    return undefined
  }
}

export function sourceRepositoryLabel(repositoryUrl: string): string {
  const url = parseHTTPURL(repositoryUrl)
  if (!url) return ''
  const path = url.pathname.replace(/^\/+|\/+$/g, '').replace(/\.git$/, '')
  return path || url.hostname
}

export function sourceBrowseUrl(source: ResourceSource | undefined): string {
  const url = source && parseHTTPURL(source.repository_url)
  if (!source || !url || !/^[0-9a-f]{40}$/.test(source.commit)) return ''
  const base = `${url.origin}${url.pathname.replace(/\/+$/, '').replace(/\.git$/, '')}`
  const segments = source.path.split('/')
  if (segments.some(segment => segment === '' || segment === '.' || segment === '..')) return base
  const path = segments.map(segment => encodeURIComponent(segment)).join('/')
  const host = url.hostname.toLowerCase()
  if (host === 'github.com' || host.startsWith('github.')) return `${base}/blob/${source.commit}/${path}`
  if (host === 'gitlab.com' || host.startsWith('gitlab.')) return `${base}/-/blob/${source.commit}/${path}`
  if (host === 'bitbucket.org') return `${base}/src/${source.commit}/${path}`
  return base
}

/**
 * Hex characters kept after the algorithm prefix when a digest is shortened.
 * Twelve is enough to recognize a revision by eye while keeping a 71-character
 * value from dominating the layout that prints it.
 */
export const SHORT_DIGEST_HEX_LENGTH = 12

/**
 * Shorten a canonical `sha256:<hex>` digest to its algorithm prefix plus a
 * recognizable hex prefix. Values that are not canonical digests are returned
 * unchanged so nothing is silently misrepresented. Callers keep the full value
 * reachable through a `title` attribute.
 */
export function shortDigest(value: string): string {
  if (!isSha256Digest(value)) return value
  return `${value.slice(0, 'sha256:'.length + SHORT_DIGEST_HEX_LENGTH)}…`
}

/** Cells per side of a resource fingerprint. */
export const FINGERPRINT_SIDE = 3

/**
 * Ink weights a fingerprint cell may take.
 *
 * Quantized rather than continuous: mapping a byte straight to an opacity
 * makes every mark an identical mid-grey mush at 26px. Four well-separated
 * steps keep each cell crisp, and 4^9 combinations stay far larger than any
 * catalog this indexes, so marks remain distinguishable.
 */
export const FINGERPRINT_LEVELS = [0.1, 0.38, 0.68, 1] as const

/**
 * Derive a stable visual mark from a content digest.
 *
 * The mark is the point of content addressing made visible: the same content
 * always produces the same mark, and any change to the content produces a
 * different one. It reads left-to-right, top-to-bottom, one ink weight per
 * cell, taken from successive hex pairs of the digest.
 *
 * Returns an empty array for anything that is not a canonical digest, so a
 * caller renders nothing rather than a mark that means nothing.
 */
export function fingerprintCells(digest: string): number[] {
  if (!isSha256Digest(digest)) return []
  const hex = digest.slice('sha256:'.length)
  const cells: number[] = []
  for (let i = 0; i < FINGERPRINT_SIDE * FINGERPRINT_SIDE; i++) {
    const byte = parseInt(hex.slice(i * 2, i * 2 + 2), 16)
    cells.push(FINGERPRINT_LEVELS[byte % FINGERPRINT_LEVELS.length]!)
  }
  return cells
}

export type FindingDetails = {
  index: number
  code: string
  severity: string
  policy: string
  message: string
  path: string
  startByte?: number
  endByte?: number
  line?: number
  column?: number
}

/** Normalize the findings from the revision's authoritative assessment. */
export function findingDetails(value: unknown, index: number): FindingDetails {
  const record = isRecord(value) ? value : {}
  const location = isRecord(record.location) ? record.location : {}
  return {
    index,
    code: stringField(record.code) || 'Unspecified finding',
    severity: stringField(record.severity) || 'Unspecified',
    policy: stringField(record.policy),
    message: stringField(record.message),
    path: stringField(location.path),
    startByte: nonNegativeInteger(location.start_byte),
    endByte: nonNegativeInteger(location.end_byte),
    line: nonNegativeInteger(location.line),
    column: nonNegativeInteger(location.column),
  }
}

export function assessmentFindings(findings: readonly unknown[] | undefined): FindingDetails[] {
  return (findings ?? []).map(findingDetails)
}

export type ResourceAvailability = 'available' | 'operator-exposed' | 'revoked' | 'policy-blocked' | 'no-artifact'

export type ResourceAvailabilityCopy = {
  /** Text that states the revision's install status. */
  label: string
  color: 'success' | 'error' | 'warning' | 'neutral'
  /** True only when the pinned install command may be offered as usable. */
  installable: boolean
  /** Sentence shown in place of the install command. Empty when installable. */
  installNotice: string
}

export const RESOURCE_AVAILABILITY: Record<ResourceAvailability, ResourceAvailabilityCopy> = {
  'available': {
    label: 'Available',
    color: 'success',
    installable: true,
    installNotice: '',
  },
  'operator-exposed': {
    label: 'Operator-exposed rejection',
    color: 'warning',
    installable: true,
    installNotice: '',
  },
  'revoked': {
    label: 'Revoked',
    color: 'error',
    installable: false,
    installNotice: 'Installation is disabled because this revision is revoked. Metadata and inspection remain available.',
  },
  'policy-blocked': {
    label: 'Blocked by policy',
    color: 'error',
    installable: false,
    installNotice: 'Installation is disabled because the current assessment rejects this revision and blocks its artifact from serving. Metadata and inspection remain available.',
  },
  'no-artifact': {
    label: 'No installable artifact',
    color: 'warning',
    installable: false,
    installNotice: 'Installation is disabled because this Marketplace stores no artifact for this revision. Metadata and inspection remain available.',
  },
}

type RevisionAvailabilityFields = Pick<ResourceRevision, 'archive_digest' | 'assessment' | 'revocation' | 'current_availability'>

/** True when the Marketplace has marked this exact revision as revoked. */
export function isRevoked(revision: Pick<ResourceRevision, 'revocation'>): boolean {
  return Boolean(revision.revocation && Object.keys(revision.revocation).length)
}

/**
 * Derive installability from the authoritative assessment decision, operator
 * revocation, and artifact presence. Finding severities are presentation data;
 * they never override the assessment decision.
 */
export function resourceAvailability(revision: RevisionAvailabilityFields, acquisition?: string, kind?: ResourceKind): ResourceAvailability {
  if (isRevoked(revision) || revision.current_availability?.availability === 'revoked') return 'revoked'
  if (revision.current_availability?.availability === 'operator-exposed') return 'operator-exposed'
  if (revision.assessment.decision === 'reject') return 'policy-blocked'
  if (!revision.archive_digest && acquisition !== 'index-only' && kind !== 'collection') return 'no-artifact'
  return 'available'
}

/** Badge and gating copy for a revision's derived install status. */
export function resourceAvailabilityCopy(revision: RevisionAvailabilityFields, acquisition?: string, kind?: ResourceKind): ResourceAvailabilityCopy {
  return RESOURCE_AVAILABILITY[resourceAvailability(revision, acquisition, kind)]
}

/**
 * Install status as reported by the listing endpoint. The listing collapses the
 * detail page's `policy-blocked` and `no-artifact` into one `blocked` value, so
 * a card states the outcome and the resource page states the reason.
 */
export type ListAvailability = NonNullable<ResourceListItem['availability']>

const LIST_AVAILABILITY: Record<ListAvailability, ResourceAvailabilityCopy> = {
  available: RESOURCE_AVAILABILITY.available,
  'operator-exposed': RESOURCE_AVAILABILITY['operator-exposed'],
  revoked: {
    ...RESOURCE_AVAILABILITY.revoked,
    label: 'Not available',
    color: 'neutral',
  },
  'policy-blocked': {
    ...RESOURCE_AVAILABILITY['policy-blocked'],
    label: 'Not available',
    color: 'neutral',
  },
  'no-artifact': {
    ...RESOURCE_AVAILABILITY['no-artifact'],
    label: 'Not available',
    color: 'neutral',
  },
  blocked: {
    label: 'Not available',
    color: 'neutral',
    installable: false,
    installNotice: 'This revision is listed for inspection but cannot be installed from this Marketplace. Open the resource to see why.',
  },
}

/**
 * Badge and gating copy for a listed resource, or null when the instance did
 * not report a status. An absent value means unknown, never available, so the
 * caller shows no status rather than asserting one.
 */
export function listAvailabilityCopy(item: Pick<ResourceListItem, 'availability'>): ResourceAvailabilityCopy | null {
  const reported = item.availability
  if (!reported) return null
  return LIST_AVAILABILITY[reported] ?? null
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value))
}

function stringField(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function nonNegativeInteger(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0 ? value : undefined
}

function hasUnsafeURLCharacters(value: string): boolean {
  return [...value].some(char => {
    const code = char.charCodeAt(0)
    return code <= 0x1f || code === 0x7f || ' \'"`$;&|<>()[]{}*!?#\\'.includes(char)
  })
}
