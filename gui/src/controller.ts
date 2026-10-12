import { reactive } from 'vue'
import {
  EventCursor,
  RequestError,
  type Action,
  type Client,
  type Configuration,
  type DiscoveryPayload,
  type GUIEvent,
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
export const DISCOVERY_REQUEST_ERROR_MESSAGE = 'Marketplace discovery request failed; retry or check the endpoint'

const FATAL_EVENT_STATUSES = new Set([400, 401, 403, 405])

export interface WorkspaceState {
  stage: string
  drift: string
  rows: Row[]
  groups: Snapshot['groups']
  harnesses: string[]
  diagnostics: Diagnostic[]
  pendingChanges: number
  migrationOffered: boolean
  discoveryEnabled: boolean
  discoveryHealth: Snapshot['discoveryHealth']
  discoveryQuery: string
  discoveryRequestId: number
  discoverySequence: number
  discoveryLoading: boolean
  discoveryError: string | null
  phase: string | null
  operation: OperationRef | null
  notifications: { id: number; severity: string; message: string }[]
  requesting: boolean
  error: string | null
  configuration: Configuration | null
  latestVersion: string
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
    groups: [],
    harnesses: [],
    diagnostics: [],
    pendingChanges: 0,
    migrationOffered: false,
    discoveryEnabled: false,
    discoveryHealth: [],
    discoveryQuery: '',
    discoveryRequestId: 0,
    discoverySequence: 0,
    discoveryLoading: false,
    discoveryError: null,
    phase: null,
    operation: null,
    notifications: [],
    requesting: false,
    error: null,
    configuration: null,
    latestVersion: '',
  })

  private readonly cursor = new EventCursor()
  private readonly timers = new Set<ReturnType<typeof setTimeout>>()
  private lastSettled: OperationRef | null = null
  private pendingQuitId: number | null = null
  private quitEpoch = 0
  private configurationSeq = 0
  private releaseSeq = 0
  private accepted = false
  private nextNotificationId = 1
  private abort: AbortController | null = null
  private discoveryAbort: AbortController | null = null
  private snapshotGeneration = 0
  private browserDiscoveryRequestId = 0
  private currentDiscoveryQuery = ''
  private lastDispatchedDiscoveryRequestId = 0
  private disposed = false

  constructor(
    private readonly client: Client,
    private readonly notifyTtl: number = NOTIFICATION_TTL_MS,
    private readonly onQuit?: () => void,
  ) {}

  async connect(): Promise<void> {
    if (this.disposed) {
      return
    }
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

  async checkRelease(): Promise<void> {
    const seq = ++this.releaseSeq
    try {
      const notice = await this.client.release()
      if (seq !== this.releaseSeq) {
        return
      }
      this.state.latestVersion = notice.latestVersion ?? ''
    } catch {
      if (seq === this.releaseSeq) {
        this.state.latestVersion = ''
      }
    }
  }

  dispose(): void {
    this.disposed = true
    this.releaseSeq++
    this.cancelQuit()
    this.abort?.abort()
    this.cancelDiscoveryRequest()
    for (const timer of this.timers) {
      clearTimeout(timer)
    }
    this.timers.clear()
  }

  setDiscoveryQuery(query: string): void {
    this.issueDiscoveryRequest(query, false)
  }

  retryDiscovery(): void {
    if (this.state.discoveryEnabled) {
      this.issueDiscoveryRequest(this.currentDiscoveryQuery, true)
    }
  }

  private issueDiscoveryRequest(query: string, retry: boolean): void {
    if (this.disposed || (!retry && query === this.currentDiscoveryQuery)) {
      return
    }
    this.currentDiscoveryQuery = query
    this.browserDiscoveryRequestId++
    this.state.discoveryQuery = query
    this.state.discoveryRequestId = this.browserDiscoveryRequestId
    this.state.discoveryHealth = []
    this.state.discoveryLoading = this.state.discoveryEnabled
    this.state.discoveryError = null
    this.cancelDiscoveryRequest()
    this.dispatchDiscoveryRequest()
  }

  async refresh(): Promise<void> {
    if (this.state.requesting || this.state.operation != null) {
      return
    }
    this.state.requesting = true
    try {
      await this.client.snapshot()
      await this.loadConfiguration()
      await this.checkRelease()
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

  browse(row: Row): void {
    this.reportTelemetry({ action: 'browse', rowId: row.id })
  }

  recordSyncQuitCancellation(): void {
    this.reportTelemetry({ action: 'modal_outcome', modal: 'sync+quit', outcome: 'cancelled' })
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

  private reportTelemetry(event: GUIEvent): void {
    void this.client.telemetry(event).catch(() => undefined)
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
    if (this.disposed) {
      return
    }
    switch (event.type) {
      case 'snapshot': {
        this.applySnapshot(event.payload, event.operation ?? null, event.generation)
        break
      }
      case 'discovery': {
        this.applyDiscovery(event.generation, event.payload)
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

  private applySnapshot(snapshot: Snapshot, operation: OperationRef | null, generation: number): void {
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
      this.applySnapshotFields(snapshot, generation)
      if (quitNow) {
        this.onQuit?.()
      }
      return
    }
    this.applySnapshotFields(snapshot, generation)
  }

  private applySnapshotFields(snapshot: Snapshot, generation: number): void {
    this.snapshotGeneration = generation
    this.state.stage = snapshot.stage
    this.state.drift = snapshot.drift
    this.state.groups = snapshot.groups
    this.state.harnesses = snapshot.harnesses
    this.state.diagnostics = snapshot.diagnostics
    this.state.pendingChanges = snapshot.pendingChanges ?? 0
    this.state.migrationOffered = snapshot.migrationOffered === true
    const wasDiscoveryEnabled = this.state.discoveryEnabled
    const pendingDiscoveryIntent = this.browserDiscoveryRequestId > snapshot.discoveryRequestId
    this.state.discoveryEnabled = snapshot.discoveryEnabled === true
    this.browserDiscoveryRequestId = Math.max(this.browserDiscoveryRequestId, snapshot.discoveryRequestId)
    const currentDiscovery =
      this.state.discoveryEnabled &&
      snapshot.discoveryRequestId === this.browserDiscoveryRequestId &&
      snapshot.discoveryQuery === this.currentDiscoveryQuery
    this.state.discoveryQuery = this.currentDiscoveryQuery
    this.state.discoveryRequestId = this.browserDiscoveryRequestId
    if (currentDiscovery) {
      this.state.discoverySequence = Math.max(this.state.discoverySequence, snapshot.discoverySequence)
      this.state.discoveryHealth = snapshot.discoveryHealth
      this.state.discoveryLoading = snapshot.discoveryLoading
      this.state.discoveryError = null
    } else {
      this.state.discoveryHealth = []
      this.state.discoveryLoading = false
    }
    if (!this.state.discoveryEnabled) {
      this.cancelDiscoveryRequest()
      this.lastDispatchedDiscoveryRequestId = 0
    }
    const rows = currentDiscovery ? snapshot.rows : snapshot.rows.filter((row) => !row.discovery)
    if (snapshot.stage === 'final' || !this.accepted) {
      this.state.rows = rows
    }
    if (snapshot.stage === 'final') {
      this.accepted = true
      if (this.state.error === EVENT_OUTAGE_MESSAGE) {
        this.state.error = null
      }
    }
    if (this.state.discoveryEnabled && !wasDiscoveryEnabled && pendingDiscoveryIntent) {
      this.dispatchDiscoveryRequest()
    }
  }

  private applyDiscovery(generation: number, payload: DiscoveryPayload): void {
    if (
      !this.state.discoveryEnabled ||
      generation !== this.snapshotGeneration ||
      payload.requestId !== this.browserDiscoveryRequestId ||
      payload.query !== this.currentDiscoveryQuery ||
      payload.discoverySequence <= this.state.discoverySequence
    ) {
      return
    }
    this.state.rows = payload.rows
    this.state.discoveryHealth = payload.health
    this.state.discoveryQuery = payload.query
    this.state.discoveryRequestId = payload.requestId
    this.state.discoverySequence = payload.discoverySequence
    this.state.discoveryLoading = payload.loading
    this.state.discoveryError = null
  }

  private dispatchDiscoveryRequest(): void {
    if (this.disposed || !this.state.discoveryEnabled || this.browserDiscoveryRequestId === 0 || this.lastDispatchedDiscoveryRequestId === this.browserDiscoveryRequestId) {
      return
    }
    this.cancelDiscoveryRequest()
    const controller = new AbortController()
    const requestId = this.browserDiscoveryRequestId
    const query = this.currentDiscoveryQuery
    this.state.discoveryLoading = true
    this.state.discoveryError = null
    this.discoveryAbort = controller
    this.lastDispatchedDiscoveryRequestId = requestId
    void this.client.discovery(query, requestId, controller.signal).catch(() => {
      if (controller.signal.aborted || requestId !== this.browserDiscoveryRequestId || query !== this.currentDiscoveryQuery) {
        return
      }
      this.state.discoveryError = DISCOVERY_REQUEST_ERROR_MESSAGE
      this.state.discoveryLoading = false
    })
  }

  private cancelDiscoveryRequest(): void {
    this.discoveryAbort?.abort()
    this.discoveryAbort = null
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
