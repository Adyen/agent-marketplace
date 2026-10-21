import { describe, expect, it } from 'vitest'
import { notificationPresentation, operationDetail, operationLabel } from './feedback'

describe('operation feedback labels', () => {
  it('humanizes known operation kinds and capitalizes unknown ones', () => {
    expect(operationLabel('sync')).toBe('Syncing')
    expect(operationLabel('update')).toBe('Updating')
    expect(operationLabel('targets')).toBe('Saving harness targets')
    expect(operationLabel('mutation')).toBe('Applying changes')
    expect(operationLabel('repair')).toBe('Repair')
    expect(operationLabel('constructor')).toBe('Constructor')
  })

  it('shows an observed phase and the generic state when no phase was observed', () => {
    expect(operationDetail('materialize')).toBe('materialize')
    expect(operationDetail('running')).toBe('Working…')
    expect(operationDetail('')).toBe('Working…')
    expect(operationDetail(null)).toBe('Working…')
  })

  it('maps notification severities to icon and tone, with success for completion messages', () => {
    expect(notificationPresentation({ id: 1, severity: 'info', message: 'sync complete' })).toEqual({ icon: 'check', tone: 'success' })
    expect(notificationPresentation({ id: 2, severity: 'info', message: 'still working' })).toEqual({ icon: 'info', tone: 'info' })
    expect(notificationPresentation({ id: 3, severity: 'warning', message: 'stale lock' })).toEqual({ icon: 'warning', tone: 'warning' })
    expect(notificationPresentation({ id: 4, severity: 'error', message: 'mutation failed' })).toEqual({ icon: 'error', tone: 'danger' })
  })
})
