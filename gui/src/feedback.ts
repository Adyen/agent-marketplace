import type { NotificationOutcome, ResourceAction } from './api/client'

type Notification = { id: number; severity: string; outcome?: NotificationOutcome; message: string }

const operationLabels = new Map<string, string>([
  ['sync', 'Syncing…'],
  ['update', 'Updating…'],
  ['targets', 'Saving harness targets…'],
  ['mutation', 'Applying changes…'],
])

const resourceActionProgressVerbs: Partial<Record<ResourceAction, string>> = {
  install: 'Installing',
  remove: 'Removing',
  exclude: 'Excluding',
  restore: 'Restoring',
}

export function resourceActionProgressLabel(action: ResourceAction, displayName: string): string {
  const verb = resourceActionProgressVerbs[action]
  return verb == null ? `Checking ${displayName}…` : `${verb} ${displayName}…`
}

export function operationLabel(kind: string, action?: ResourceAction, displayName?: string): string {
  if (action != null && displayName != null && action !== 'none') {
    return resourceActionProgressLabel(action, displayName)
  }
  const label = operationLabels.get(kind)
  if (label !== undefined) {
    return label
  }
  return `${kind.charAt(0).toUpperCase()}${kind.slice(1)}…`
}

export function operationDetail(phase: string | null): string {
  return phase == null || phase === '' || phase === 'running' ? '' : phase
}

export function notificationPresentation(notification: Notification): { icon: 'check' | 'info' | 'warning' | 'error'; tone: 'success' | 'info' | 'warning' | 'danger' } {
  switch (notification.outcome) {
    case 'success':
      return { icon: 'check', tone: 'success' }
    case 'warning':
      return { icon: 'warning', tone: 'warning' }
    case 'failure':
      return { icon: 'error', tone: 'danger' }
  }
  switch (notification.severity) {
    case 'error':
      return { icon: 'error', tone: 'danger' }
    case 'warning':
      return { icon: 'warning', tone: 'warning' }
    default:
      return { icon: 'info', tone: 'info' }
  }
}
