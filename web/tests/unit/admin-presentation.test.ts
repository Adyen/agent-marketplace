import { describe, expect, it } from 'vitest'
import { absoluteAdminTime, applicableLifecycleActions, lifecycleBadge, operationLabel, outcomeBadge, relativeAdminTime, shortId } from '../../app/lib/admin-presentation'

const now = new Date('2026-09-19T12:00:00Z')

describe('admin presentation helpers', () => {
  it('formats relative and absolute times without hiding unparsable input', () => {
    expect(relativeAdminTime('2026-09-19T11:59:40Z', now)).toBe('just now')
    expect(relativeAdminTime('2026-09-19T11:58:00Z', now)).toBe('2 minutes ago')
    expect(relativeAdminTime('2026-09-19T09:00:00Z', now)).toBe('3 hours ago')
    expect(relativeAdminTime('2026-09-17T12:00:00Z', now)).toBe('2 days ago')
    expect(relativeAdminTime('not-a-date', now)).toBe('not-a-date')
    expect(relativeAdminTime(undefined, now)).toBe('Unknown time')
    expect(absoluteAdminTime('2026-09-19T09:05:00Z')).toBe('2026-09-19 09:05:00 UTC')
    expect(absoluteAdminTime('garbage')).toBe('garbage')
  })

  it('shortens UUIDs and hash-suffixed identifiers but keeps readable ones', () => {
    expect(shortId('a1b2c3d4-e5f6-7890-abcd-ef1234567890')).toBe('a1b2c3d4')
    expect(shortId('managed-refresh-1061fe1dcaedcb54bf1a7c4c6ce3b43925d3ddeebe3f5b31bec40ca7a06998d9')).toBe('managed-refresh-1061fe1d')
    expect(shortId('job-1')).toBe('job-1')
    expect(shortId('anthropic-skills-refresh-child')).toBe('anthropic-skills-refresh-child')
    expect(shortId(undefined)).toBe('')
  })

  it('maps lifecycle and outcomes to badges with a readable fallback', () => {
    expect(lifecycleBadge('active')).toEqual({ label: 'Active', color: 'success' })
    expect(lifecycleBadge('withdrawn').color).toBe('error')
    expect(lifecycleBadge('something_new')).toEqual({ label: 'Something new', color: 'neutral' })
    expect(outcomeBadge('failed').color).toBe('error')
    expect(outcomeBadge('queued')).toEqual({ label: 'Queued', color: 'neutral' })
    expect(outcomeBadge(undefined)).toEqual({ label: 'Unknown', color: 'neutral' })
  })

  it('offers only lifecycle actions that apply to the current state', () => {
    expect(applicableLifecycleActions({ paused: false, withdrawn: false, retired: false })).toEqual(['pause', 'reindex', 'withdraw'])
    expect(applicableLifecycleActions({ paused: true, withdrawn: false, retired: false })).toEqual(['resume', 'reindex', 'withdraw'])
    expect(applicableLifecycleActions({ paused: true, withdrawn: true, retired: false })).toEqual(['restore', 'retire'])
    expect(applicableLifecycleActions({ paused: true, withdrawn: true, retired: true })).toEqual(['restore_retired'])
  })

  it('labels operations for humans', () => {
    expect(operationLabel('upsert_registry')).toBe('Registry settings changed')
    expect(operationLabel('configuration.adopt')).toBe('Configuration adopted')
    expect(operationLabel('unknown_op')).toBe('Unknown op')
  })
})
