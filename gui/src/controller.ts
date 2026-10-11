import { reactive } from 'vue'
import {
  EventCursor,
  RequestError,
  type Action,
  type Client,
  type Configuration,
  type OperationRef,
  type SessionEvent,
  type Snapshot,
  type SourceInput,
} from './api/client'

export type Row = Snapshot['rows'][number]
export type Diagnostic = Snapshot['diagnostics'][number]

export const MAX_NOTIFICATIONS = 3
export const NOTIFICATION_TTL_MS = 6000
export const EVENT_RETRY_DELAY_MS = 250
export const EVENT_OUTAGE_MESSAGE = 'Session updates are unavailable'

const FATAL_EVENT_STATUSES = new Set([400, 401, 403, 405])

export interface WorkspaceState {
  stage: string
  drift: string
  rows: Row[]
  harnesses: string[]
  diagnostics: Diagnostic[]
  pendingChanges: number
  migrationOffered: boolean
  phase: string | null
  operation: OperationRef | null
  notifications: { id: number; severity: string; message: string }[]
  requesting: boolean
  error: string | null
  configuration: Configuration | null
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
    pendingChanges: 0,
    migrationOffered: false,
    phase: null,
    operation: null,
    notifications: [],
    requesting: false,
    error: null,
    configuration: null,
  })

  private readonly cursor = new EventCursor()
  private readonly timers = new Set<ReturnType<typeof setTimeout>>()
  private lastSettled: OperationRef | null = null
  private pendingQuitId: number | null = null
  private quitEpoch = 0
  private configurationSeq = 0
  private accepted = false
  private nextNotificationId = 1
  private abort: AbortController | null = null

  constructor(
    private readonly client: Client,
    private readonly notifyTtl: number = NOTIFICATION_TTL_MS,
    private readonly onQuit?: () => void,
  ) {}

  async connect(): Promise<void> {
    if (this.abort != null && !this.abort.signal.aborted) {
      return
    }
    const abort = new AbortController()
    this.abort = abort
    for (;;) {
      try {
        for await (const event of this.client.events(this.cursor, abort.signal)) {
          this.apply(event)
        }
      } catch (error) {
        if (abort.signal.aborted) {
          return
        }
        if (error instanceof RequestError && FATAL_EVENT_STATUSES.has(error.status)) {
          throw error
        }
      }
      if (abort.signal.aborted) {
        return
      }
      this.state.error = EVENT_OUTAGE_MESSAGE
      await new Promise<void>((resolve) => {
        const timer = setTimeout(() => {
          abort.signal.removeEventListener('abort', onAbort)
          resolve()
        }, EVENT_RETRY_DELAY_MS)
        const onAbort = () => {
          clearTimeout(timer)
          resolve()
        }
        abort.signal.addEventListener('abort', onAbort, { once: true })
      })
      if (abort.signal.aborted) {
        return
      }
    }
  }

  dispose(): void {
    this.cancelQuit()
    this.abort?.abort()
    for (const timer of this.timers) {
      clearTimeout(timer)
    }
    this.timers.clear()
  }

  async refresh(): Promise<void> {
    if (this.state.requesting || this.state.operation != null) {
      return
    }
    this.state.requesting = true
    try {
      await this.client.snapshot()
      await this.loadConfiguration()
    } catch {
      this.state.error ??= 'Refresh failed'
    } finally {
      this.state.requesting = false
    }
  }

  async loadConfiguration(): Promise<void> {
    const seq = ++this.configurationSeq
    try {
      const configuration = await this.client.configuration()
      if (seq !== this.configurationSeq) {
        return
      }
      this.state.configuration = configuration
    } catch {
      if (seq === this.configurationSeq && this.state.configuration == null) {
        this.state.error ??= 'Settings could not be loaded'
      }
    }
  }

  async runAction(action: Action): Promise<boolean> {
    return this.requestOperation(() => this.client.action(action))
  }

  async syncAndQuit(): Promise<boolean> {
    const active = this.state.operation
    if (active != null) {
      if (active.kind !== 'sync') {
        return false
      }
      this.pendingQuitId = active.id
      return true
    }
    return this.requestOperation(() => this.client.action('sync'), true)
  }

  quit(): void {
    this.cancelQuit()
    this.onQuit?.()
  }

  cancelQuit(): void {
    this.pendingQuitId = null
    this.quitEpoch++
  }

  async saveTargets(targets: string[]): Promise<boolean> {
    return this.requestOperation(() => this.client.configure({ action: 'save-targets', targets }))
  }

  async addSource(source: SourceInput): Promise<boolean> {
    return this.requestOperation(() => this.client.configure({ action: 'add-source', source }))
  }

  async removeSource(id: string): Promise<boolean> {
    return this.requestOperation(() => this.client.configure({ action: 'remove-source', id }))
  }

  async initialize(targets: string[], consent?: boolean, sources: SourceInput[] = []): Promise<boolean> {
    return this.requestOperation(() =>
      this.client.configure({
        action: 'initialize',
        targets,
        ...(consent === undefined ? {} : { consent }),
        ...(sources.length === 0 ? {} : { sources }),
      }),
    )
  }

  private async requestOperation(start: () => Promise<OperationRef>, quitAfter = false): Promise<boolean> {
    if (this.state.operation != null || this.state.requesting) {
      return false
    }
    this.state.requesting = true
    const quitEpoch = this.quitEpoch
    let operation: OperationRef
    try {
      operation = await start()
    } catch (error) {
      this.state.requesting = false
      if (error instanceof RequestError && (error.status === 409 || error.code === 'localapi.busy')) {
        return false
      }
      this.state.error = 'The action could not be started'
      return false
    }
    this.state.requesting = false
    if (operation.id > (this.lastSettled?.id ?? 0) && this.state.operation == null) {
      this.state.operation = operation
      this.state.phase = null
    }
    if (quitAfter && quitEpoch === this.quitEpoch) {
      const settled = this.lastSettled
      if (settled != null && settled.id === operation.id && settled.kind === 'sync') {
        this.quitEpoch++
        this.onQuit?.()
      } else {
        this.pendingQuitId = operation.id
      }
    }
    return true
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
      if (this.lastSettled == null || operation.id > this.lastSettled.id) {
        if (this.state.operation?.id !== operation.id) {
          this.state.phase = null
        }
        this.state.operation = operation
      }
    } else if (snapshot.stage === 'final') {
      const reported = snapshot.settledOperation ?? null
      const settled = reported ?? this.state.operation
      let quitNow = false
      if (settled != null) {
        const last = this.lastSettled
        const active = this.state.operation
        const fresh = last == null || settled.id > last.id
        if (reported != null && last != null && settled.id < last.id) {
          return
        }
        if (reported != null && fresh && active != null && active.id !== settled.id) {
          return
        }
        if (fresh) {
          this.lastSettled = settled
          if (active != null && active.id <= settled.id) {
            this.state.operation = null
            this.state.phase = null
          }
          if (settled.kind === 'targets' || settled.kind === 'mutation' || settled.kind === 'migration') {
            void this.loadConfiguration()
          }
          if (this.pendingQuitId != null && settled.id === this.pendingQuitId && settled.kind === 'sync') {
            this.cancelQuit()
            quitNow = true
          }
        } else if (reported == null && active != null && last != null && active.id <= last.id) {
          this.state.operation = null
          this.state.phase = null
        }
      }
      this.applySnapshotFields(snapshot)
      if (quitNow) {
        this.onQuit?.()
      }
      return
    }
    this.applySnapshotFields(snapshot)
  }

  private applySnapshotFields(snapshot: Snapshot): void {
    this.state.stage = snapshot.stage
    this.state.drift = snapshot.drift
    this.state.harnesses = snapshot.harnesses
    this.state.diagnostics = snapshot.diagnostics
    this.state.pendingChanges = snapshot.pendingChanges ?? 0
    this.state.migrationOffered = snapshot.migrationOffered === true
    if (snapshot.stage === 'final' || !this.accepted) {
      this.state.rows = snapshot.rows
    }
    if (snapshot.stage === 'final') {
      this.accepted = true
      if (this.state.error === EVENT_OUTAGE_MESSAGE) {
        this.state.error = null
      }
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
