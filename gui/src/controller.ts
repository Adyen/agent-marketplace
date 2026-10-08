import { reactive } from 'vue'
import {
  EventCursor,
  RequestError,
  type Action,
  type Client,
  type OperationRef,
  type SessionEvent,
  type Snapshot,
} from './api/client'

export type Row = Snapshot['rows'][number]
export type Diagnostic = Snapshot['diagnostics'][number]

export const MAX_NOTIFICATIONS = 3
export const NOTIFICATION_TTL_MS = 6000

export interface WorkspaceState {
  stage: string
  drift: string
  rows: Row[]
  harnesses: string[]
  diagnostics: Diagnostic[]
  phase: string | null
  operation: OperationRef | null
  notifications: { id: number; severity: string; message: string }[]
  requesting: boolean
  error: string | null
}

export function groupKey(row: Row): string {
  if (row.required) {
    return 'required'
  }
  if (
    row.status.startsWith('malformed') ||
    row.status.startsWith('unavailable') ||
    row.status === 'broken' ||
    row.status === 'orphaned'
  ) {
    return 'attention'
  }
  if (row.checked) {
    return 'enabled'
  }
  return 'available'
}

export const GROUP_ORDER = ['attention', 'required', 'enabled', 'available'] as const

export function groupLabel(group: string): string {
  switch (group) {
    case 'attention':
      return 'Needs attention'
    case 'required':
      return 'Required'
    case 'enabled':
      return 'Enabled'
    default:
      return 'Available'
  }
}

const FATAL_DIAGNOSTIC_CATEGORIES = new Set(['config', 'catalog', 'resolution', 'materialization'])

export function eligible(
  state: Pick<WorkspaceState, 'stage' | 'drift' | 'harnesses' | 'diagnostics' | 'operation' | 'requesting'>,
): boolean {
  return (
    (state.stage === 'inventory' || state.stage === 'final') &&
    state.drift !== 'failed' &&
    state.harnesses.length > 0 &&
    !state.diagnostics.some((d) => d.severity === 'error' && FATAL_DIAGNOSTIC_CATEGORIES.has(d.category)) &&
    state.operation == null &&
    !state.requesting
  )
}

export class WorkspaceController {
  readonly state = reactive<WorkspaceState>({
    stage: '',
    drift: 'pending',
    rows: [],
    harnesses: [],
    diagnostics: [],
    phase: null,
    operation: null,
    notifications: [],
    requesting: false,
    error: null,
  })

  private readonly cursor = new EventCursor()
  private readonly timers = new Set<ReturnType<typeof setTimeout>>()
  private lastSettledOperationId = 0
  private accepted = false
  private nextNotificationId = 1
  private abort: AbortController | null = null

  constructor(
    private readonly client: Client,
    private readonly notifyTtl: number = NOTIFICATION_TTL_MS,
  ) {}

  async connect(): Promise<void> {
    this.abort = new AbortController()
    try {
      for await (const event of this.client.events(this.cursor, this.abort.signal)) {
        this.apply(event)
      }
    } catch (error) {
      if (this.abort.signal.aborted) {
        return
      }
      this.state.error = 'Session updates are unavailable'
      throw error
    }
  }

  dispose(): void {
    this.abort?.abort()
    for (const timer of this.timers) {
      clearTimeout(timer)
    }
    this.timers.clear()
  }

  async refresh(): Promise<void> {
    this.state.requesting = true
    try {
      await this.client.snapshot()
    } catch {
      this.state.error ??= 'Refresh failed'
    } finally {
      this.state.requesting = false
    }
  }

  async runAction(action: Action): Promise<void> {
    if (this.state.operation != null || this.state.requesting) {
      return
    }
    this.state.requesting = true
    let operation: OperationRef
    try {
      operation = await this.client.action(action)
    } catch (error) {
      this.state.requesting = false
      if (error instanceof RequestError && (error.status === 409 || error.code === 'localapi.busy')) {
        return
      }
      this.state.error = 'The action could not be started'
      return
    }
    this.state.requesting = false
    if (operation.id <= this.lastSettledOperationId || this.state.operation != null) {
      return
    }
    this.state.operation = operation
    this.state.phase = null
  }

  private apply(event: SessionEvent): void {
    switch (event.type) {
      case 'snapshot': {
        this.applySnapshot(event.payload, event.operation ?? null)
        break
      }
      case 'progress': {
        if (this.state.operation != null && event.operation != null && event.operation.id === this.state.operation.id) {
          this.state.phase = event.payload.phase || 'running'
        }
        break
      }
      case 'notification': {
        this.notify(event.payload.severity, event.payload.message)
        break
      }
    }
  }

  private applySnapshot(snapshot: Snapshot, operation: OperationRef | null): void {
    if (operation != null) {
      if (this.state.operation?.id !== operation.id) {
        this.state.phase = null
      }
      this.state.operation = operation
    } else if (snapshot.stage === 'final') {
      if (this.state.operation != null) {
        this.lastSettledOperationId = this.state.operation.id
      }
      this.state.operation = null
      this.state.phase = null
    }
    this.state.stage = snapshot.stage
    this.state.drift = snapshot.drift
    this.state.harnesses = snapshot.harnesses
    this.state.diagnostics = snapshot.diagnostics
    if (snapshot.stage === 'final' || !this.accepted) {
      this.state.rows = snapshot.rows
    }
    if (snapshot.stage === 'final') {
      this.accepted = true
    }
  }

  private notify(severity: string, message: string): void {
    const id = this.nextNotificationId++
    this.state.notifications.push({ id, severity, message })
    while (this.state.notifications.length > MAX_NOTIFICATIONS) {
      this.state.notifications.shift()
    }
    const timer = setTimeout(() => {
      this.timers.delete(timer)
      const index = this.state.notifications.findIndex((entry) => entry.id === id)
      if (index >= 0) {
        this.state.notifications.splice(index, 1)
      }
    }, this.notifyTtl)
    this.timers.add(timer)
  }
}
