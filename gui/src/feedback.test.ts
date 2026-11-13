import { describe, expect, it } from 'vitest'
import { notificationPresentation, operationDetail, operationLabel } from './feedback'

describe('operation feedback labels', () => {
  it('humanizes known operation kinds and capitalizes unknown ones', () => {
    expect(operationLabel('sync')).toBe('Syncing…')
    expect(operationLabel('update')).toBe('Updating…')
    expect(operationLabel('targets')).toBe('Saving harness targets…')
    expect(operationLabel('mutation')).toBe('Applying changes…')
    expect(operationLabel('mutation', 'install', 'review')).toBe('Installing review…')
    expect(operationLabel('repair')).toBe('Repair…')
    expect(operationLabel('constructor')).toBe('Constructor…')
  })

// spec: gui-shell/progress-names-the-action-without-invented-phases
  it('shows only an observed phase', () => {
    expect(operationDetail('materialize')).toBe('materialize')
    expect(operationDetail('running')).toBe('')
    expect(operationDetail('')).toBe('')
    expect(operationDetail(null)).toBe('')
  })

  // spec: gui-local-api/outcome-drives-notification-tone
  it('chooses notification tone from the typed outcome', () => {
    expect(notificationPresentation({ id: 1, severity: 'info', outcome: 'success', message: 'Installed review' })).toEqual({ icon: 'check', tone: 'success' })
    expect(notificationPresentation({ id: 2, severity: 'error', outcome: 'warning', message: 'Installed review with 2 warning(s)' })).toEqual({ icon: 'warning', tone: 'warning' })
    expect(notificationPresentation({ id: 3, severity: 'info', outcome: 'failure', message: 'Sync complete' })).toEqual({ icon: 'error', tone: 'danger' })
    expect(notificationPresentation({ id: 4, severity: 'info', message: 'Sync complete' })).toEqual({ icon: 'info', tone: 'info' })
  })
})
