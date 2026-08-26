import type { ResourceKind, ResourceListItem, ResourceRevision } from '~/types/marketplace'
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

export function sourceLabel(item: ResourceListItem): string {
  return item.source_slug || item.source_id
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

export function resourceLatestLocator(item: ResourceListItem, browserBase: string): string {
  const canonical = safeCanonicalUrl(browserBase)
  const source = item.source_slug || item.source_id
  if (!canonical || !isSafeRouteToken(item.source_id) || (item.source_slug !== undefined && !isSafeRouteToken(item.source_slug)) || !isSafeResourceRoute(source, item.kind, item.name)) return ''
  return `${canonical}/s/${encodeURIComponent(source)}/${encodeURIComponent(item.kind)}/${encodeURIComponent(item.name)}`
}

export function resourceLocator(item: ResourceListItem, browserBase: string): string {
  const latest = resourceLatestLocator(item, browserBase)
  if (!latest || !isSha256Digest(item.content_digest)) return ''
  return `${latest}/revisions/${encodeURIComponent(item.content_digest)}`
}

export function installCommand(item: ResourceListItem, browserBase: string): string {
  const locator = resourceLocator(item, browserBase)
  return locator ? `agent-marketplace install "${locator}"` : ''
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
}

/** Severity the analyzer reports when its policy rejects a revision. */
const REJECT_SEVERITY = 'reject'

/**
 * Normalize the analyzer findings carried by a revision or an inspection
 * manifest. Findings are unvalidated remote data, so every field is coerced to
 * a bounded, renderable shape.
 */
export function findingDetails(value: unknown, index: number): FindingDetails {
  const record = isRecord(value) ? value : {}
  return {
    index,
    code: stringField(record.code) || 'Unspecified finding',
    severity: stringField(record.severity) || 'Unspecified',
    policy: stringField(record.policy),
    message: stringField(record.message),
    path: stringField(record.path),
    startByte: nonNegativeInteger(record.start_byte),
    endByte: nonNegativeInteger(record.end_byte),
  }
}

/**
 * Normalize several finding arrays into one list, dropping repeats. The
 * revision record and the inspection manifest project the same analyzer run:
 * the manifest adds a file path and byte range, the revision record does not.
 * Rendering both verbatim shows the reader the same warning twice with no way
 * to tell the copies apart, so an unlocated finding is dropped when the same
 * code, severity, policy, and message is already listed with a location. Pass
 * the located group first. Indexes are assigned over the merged list so they
 * stay usable as element identifiers.
 */
export function mergeFindings(...groups: Array<readonly unknown[] | undefined>): FindingDetails[] {
  const seenExact = new Set<string>()
  const seenIdentity = new Set<string>()
  const merged: FindingDetails[] = []
  for (const group of groups) {
    for (const value of group ?? []) {
      const finding = findingDetails(value, merged.length)
      const identity = [finding.code, finding.severity, finding.policy, finding.message].join('\u0000')
      const exact = [identity, finding.path, finding.startByte, finding.endByte].join('\u0000')
      if (seenExact.has(exact) || (finding.path === '' && seenIdentity.has(identity))) continue
      seenExact.add(exact)
      seenIdentity.add(identity)
      merged.push(finding)
    }
  }
  return merged
}

export type ResourceAvailability = 'available' | 'revoked' | 'policy-blocked' | 'no-artifact'

export type ResourceAvailabilityCopy = {
  /** Badge text. A label labels: it states the revision's install status. */
  label: string
  color: 'success' | 'error' | 'warning'
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
    installNotice: 'Installation is disabled because the security policy rejected this revision and removed its artifact. Metadata and inspection remain available.',
  },
  'no-artifact': {
    label: 'No installable artifact',
    color: 'warning',
    installable: false,
    installNotice: 'Installation is disabled because this Marketplace stores no artifact for this revision. Metadata and inspection remain available.',
  },
}

type RevisionAvailabilityFields = Pick<ResourceRevision, 'archive_digest' | 'findings' | 'revocation'>

/** True when the Marketplace has marked this exact revision as revoked. */
export function isRevoked(revision: Pick<ResourceRevision, 'revocation'>): boolean {
  return Boolean(revision.revocation && Object.keys(revision.revocation).length)
}

/** True when the analyzer reported at least one reject-severity finding. */
export function hasRejectFinding(findings: readonly unknown[] | undefined): boolean {
  return mergeFindings(findings).some(finding => finding.severity.toLowerCase() === REJECT_SEVERITY)
}

/**
 * Derive what a revision's install status actually is. Revocation, a
 * reject-severity finding, and a missing artifact each make a revision
 * uninstallable; only a revision with none of them may be called available.
 * Every state stays fully browsable and inspectable.
 */
export function resourceAvailability(revision: RevisionAvailabilityFields): ResourceAvailability {
  if (isRevoked(revision)) return 'revoked'
  if (hasRejectFinding(revision.findings)) return 'policy-blocked'
  if (!revision.archive_digest) return 'no-artifact'
  return 'available'
}

/** Badge and gating copy for a revision's derived install status. */
export function resourceAvailabilityCopy(revision: RevisionAvailabilityFields): ResourceAvailabilityCopy {
  return RESOURCE_AVAILABILITY[resourceAvailability(revision)]
}

/**
 * Install status as reported by the listing endpoint. The listing collapses the
 * detail page's `policy-blocked` and `no-artifact` into one `blocked` value, so
 * a card states the outcome and the resource page states the reason.
 */
export type ListAvailability = NonNullable<ResourceListItem['availability']>

const LIST_AVAILABILITY: Record<ListAvailability, ResourceAvailabilityCopy> = {
  available: RESOURCE_AVAILABILITY.available,
  revoked: RESOURCE_AVAILABILITY.revoked,
  blocked: {
    label: 'Not installable',
    color: 'warning',
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
