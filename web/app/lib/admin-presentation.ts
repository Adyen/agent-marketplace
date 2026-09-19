import type { AdminProposal } from '~/lib/admin'

export type BadgeColor = 'primary' | 'secondary' | 'success' | 'info' | 'warning' | 'error' | 'neutral'
export type Badge = { label: string, color: BadgeColor }

const RELATIVE_UNITS: [Intl.RelativeTimeFormatUnit, number][] = [['year', 31_536_000], ['month', 2_592_000], ['day', 86_400], ['hour', 3_600], ['minute', 60]]

// Human relative time for a stored UTC timestamp. Returns the raw value when
// unparsable so nothing is silently hidden.
export function relativeAdminTime(iso: string | undefined, now: Date = new Date()): string {
  if (!iso) return 'Unknown time'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  const seconds = Math.round((date.getTime() - now.getTime()) / 1000)
  if (Math.abs(seconds) < 45) return 'just now'
  const format = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
  for (const [unit, size] of RELATIVE_UNITS) {
    if (Math.abs(seconds) >= size) return format.format(Math.round(seconds / size), unit)
  }
  return format.format(Math.round(seconds / 60), 'minute')
}

export function absoluteAdminTime(iso: string | undefined): string {
  if (!iso) return 'Unknown time'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return `${date.toISOString().slice(0, 19).replace('T', ' ')} UTC`
}

// UUIDs and hash-suffixed identifiers (managed-refresh-<sha256>) keep a
// readable prefix plus the first 8 hex characters; other IDs stay intact.
// The full value is always available for copying.
export function shortId(value: string | undefined): string {
  if (!value) return ''
  if (/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value)) return value.slice(0, 8)
  const hashed = /^(.*?)([0-9a-f]{32,})$/i.exec(value)
  return hashed ? `${hashed[1]}${hashed[2]!.slice(0, 8)}` : value
}

const LIFECYCLE: Record<string, Badge> = {
  active: { label: 'Active', color: 'success' },
  paused: { label: 'Paused', color: 'warning' },
  withdrawn: { label: 'Withdrawn', color: 'error' },
  retired: { label: 'Retired', color: 'neutral' },
}
const OUTCOME: Record<string, Badge> = {
  succeeded: { label: 'Succeeded', color: 'success' },
  completed: { label: 'Completed', color: 'success' },
  failed: { label: 'Failed', color: 'error' },
  started: { label: 'Started', color: 'info' },
  pending: { label: 'Pending', color: 'neutral' },
  queued: { label: 'Queued', color: 'neutral' },
  leased: { label: 'Leased', color: 'info' },
  running: { label: 'Running', color: 'info' },
  cancelled: { label: 'Cancelled', color: 'neutral' },
  skipped: { label: 'Skipped', color: 'neutral' },
}

function fallbackBadge(value: string | undefined): Badge {
  if (!value) return { label: 'Unknown', color: 'neutral' }
  return { label: value.charAt(0).toUpperCase() + value.slice(1).replaceAll('_', ' '), color: 'neutral' }
}
export function lifecycleBadge(state: string | undefined): Badge { return LIFECYCLE[state || ''] || fallbackBadge(state) }
export function outcomeBadge(outcome: string | undefined): Badge { return OUTCOME[outcome || ''] || fallbackBadge(outcome) }
export const jobStatusBadge = outcomeBadge

// Which lifecycle actions are meaningful for a registry in its current state.
// The backend still validates; this only avoids showing obviously inapplicable buttons.
export function applicableLifecycleActions(registry: { paused: boolean, withdrawn: boolean, retired: boolean }): AdminProposal['action'][] {
  if (registry.retired) return ['restore_retired']
  if (registry.withdrawn) return ['restore', 'retire']
  return [registry.paused ? 'resume' : 'pause', 'reindex', 'withdraw']
}
export const destructiveLifecycleActions: ReadonlySet<AdminProposal['action']> = new Set(['withdraw', 'retire'])

export const operationLabels: Record<string, string> = {
  upsert_registry: 'Registry settings changed',
  set_policy: 'License policy changed',
  pause: 'Refresh paused',
  resume: 'Refresh resumed',
  withdraw: 'Availability withdrawn',
  restore: 'Availability restored',
  retire: 'Registry retired',
  restore_retired: 'Retired registry restored',
  reindex: 'Reindex requested',
  'configuration.adopt': 'Configuration adopted',
}
export function operationLabel(operation: string | undefined): string { return operationLabels[operation || ''] || fallbackBadge(operation).label }
