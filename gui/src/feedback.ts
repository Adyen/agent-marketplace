type Notification = { id: number; severity: string; message: string }

const operationLabels = new Map<string, string>([
  ['sync', 'Syncing'],
  ['update', 'Updating'],
  ['targets', 'Saving harness targets'],
  ['mutation', 'Applying changes'],
])

export function operationLabel(kind: string): string {
  const label = operationLabels.get(kind)
  if (label !== undefined) {
    return label
  }
  return kind.charAt(0).toUpperCase() + kind.slice(1)
}

export function operationDetail(phase: string | null): string {
  return phase == null || phase === '' || phase === 'running' ? 'Working…' : phase
}

export function notificationPresentation(notification: Notification): { icon: 'check' | 'info' | 'warning' | 'error'; tone: 'success' | 'info' | 'warning' | 'danger' } {
  switch (notification.severity) {
    case 'error':
      return { icon: 'error', tone: 'danger' }
    case 'warning':
      return { icon: 'warning', tone: 'warning' }
    default:
      return notification.message.endsWith(' complete') ? { icon: 'check', tone: 'success' } : { icon: 'info', tone: 'info' }
  }
}
