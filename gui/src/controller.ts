import { reactive } from 'vue'
import {
  EventCursor,
  RequestError,
  type Action,
  type Client,
  type Configuration,
  type DiscoveryPayload,
  type GUIEvent,
  type InspectionPayload,
  type InstalledSkillFilesPayload,
  type OperationRef,
  type ResourceAction,
  type ResourceActionPlan,
  type SessionEvent,
  type Snapshot,
  type SourceInput,
} from './api/client'

export type Row = Snapshot['rows'][number]
export type Diagnostic = Snapshot['diagnostics'][number]

function canInspectResource(row: Row): boolean {
  return row.inspectable || row.installedFiles
}

export interface InspectionState {
  rowId: string
  loading: boolean
  data: InspectionPayload | InstalledSkillFilesPayload | null
  error: string | null
  selectedFileId: string | null
}

export interface ResourceActionState {
  rowId: string
  loading: boolean
  plan: ResourceActionPlan | null
  error: string | null
  acceptRisk: boolean
}

export interface ResourceActionSettlementIntent {
  operation: OperationRef
  rowId: string
  selectNext: boolean
}

export const MAX_NOTIFICATIONS = 3
export const NOTIFICATION_TTL_MS = 6000
export const EVENT_RETRY_DELAY_MS = 250
export const EVENT_OUTAGE_MESSAGE = 'Session updates are unavailable'
export const DISCOVERY_REQUEST_ERROR_MESSAGE = 'Marketplace discovery request failed; retry or check the endpoint'
export const INSPECTION_UNAVAILABLE_MESSAGE = 'Verified inspection is unavailable; check the Marketplace and retry.'
export const RESOURCE_FILES_UNAVAILABLE_MESSAGE = 'Resource files are unavailable; refresh this resource and retry.'
export const RESOURCE_ACTION_UNAVAILABLE_MESSAGE = 'Resource action could not be prepared; refresh this resource and retry.'
export const RESOURCE_ACTION_FAILED_MESSAGE = 'The resource action could not be started; refresh this resource and retry.'
export const RESOURCE_ACTION_STALE_MESSAGE = 'This resource changed; prepare the action again.'
export const RESOURCE_ACTION_BUSY_MESSAGE = 'Another session operation is in progress.'

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
  inspection: InspectionState | null
  resourceAction: ResourceActionState | null
  settlementIntent: ResourceActionSettlementIntent | null
  settledMutation: ResourceActionSettlementIntent | null
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
    inspection: null,
    resourceAction: null,
    settlementIntent: null,
    settledMutation: null,
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
  private inspectionAbort: AbortController | null = null
  private inspectionEpoch = 0
  private inspectionGeneration: number | null = null
  private resourceActionAbort: AbortController | null = null
  private resourceActionEpoch = 0
  private resourceActionGeneration: number | null = null
  private resourceActionKind: ResourceAction | null = null
  private resourceActionPosting = false
  private lastMutationNotification: { operation: OperationRef; succeeded: boolean } | null = null
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

  get resourceActionSubmitting(): boolean {
    return this.resourceActionPosting
  }

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
    this.closeInspection()
    this.cancelResourceAction()
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

  openInspection(rowId: string): void {
    if (this.disposed) {
      return
    }
    const row = this.state.rows.find((candidate) => candidate.id === rowId)
    if (row == null || !canInspectResource(row)) {
      this.closeInspection()
      return
    }
    this.issueInspection(rowId)
  }

  selectInspectionFile(fileId: string): void {
    const current = this.state.inspection
    if (this.disposed || current?.data == null || !current.data.files.some((file) => file.id === fileId)) {
      return
    }
    this.issueInspection(current.rowId, fileId)
  }

  retryInspection(): void {
    const current = this.state.inspection
    if (this.disposed || current == null) {
      return
    }
    this.issueInspection(current.rowId, current.selectedFileId ?? undefined)
  }

  closeInspection(): void {
    this.inspectionEpoch++
    this.inspectionAbort?.abort()
    this.inspectionAbort = null
    this.inspectionGeneration = null
    this.state.inspection = null
  }

  openResourceAction(rowId: string): void {
    if (this.disposed || this.state.resourceAction != null || this.state.operation != null || this.state.requesting || !eligible(this.state) || this.state.configuration?.editable !== true) {
      return
    }
    const row = this.state.rows.find((candidate) => candidate.id === rowId)
    if (row == null || row.resourceAction === 'none') {
      return
    }
    this.cancelResourceAction()
    const generation = this.snapshotGeneration
    const epoch = this.resourceActionEpoch
    const abort = new AbortController()
    this.resourceActionAbort = abort
    this.resourceActionGeneration = generation
    this.resourceActionKind = row.resourceAction
    this.state.resourceAction = { rowId, loading: true, plan: null, error: null, acceptRisk: false }
    void this.client.resourcePlan(rowId, abort.signal).then((plan) => {
      if (!this.resourceActionRequestIsCurrent(rowId, generation, epoch, abort.signal)) {
        return
      }
      const currentRow = this.state.rows.find((candidate) => candidate.id === rowId)
      const current = this.state.resourceAction
      if (current == null) {
        return
      }
      if (plan.rowId !== rowId || plan.generation !== generation || plan.action !== this.resourceActionKind || currentRow?.resourceAction !== plan.action) {
        this.resourceActionAbort = null
        this.state.resourceAction = { ...current, loading: false, plan: null, error: RESOURCE_ACTION_STALE_MESSAGE, acceptRisk: false }
        return
      }
      this.resourceActionAbort = null
      this.state.resourceAction = { ...current, loading: false, plan, error: null, acceptRisk: false }
    }).catch((error: unknown) => {
      if (!this.resourceActionRequestIsCurrent(rowId, generation, epoch, abort.signal)) {
        return
      }
      this.resourceActionAbort = null
      const current = this.state.resourceAction
      if (current != null) {
        this.state.resourceAction = { ...current, loading: false, plan: null, error: resourceActionErrorMessage(error, true), acceptRisk: false }
      }
    })
  }

  retryResourceActionPlan(): void {
    const rowId = this.state.resourceAction?.rowId
    if (rowId == null || this.disposed) {
      return
    }
    this.cancelResourceAction()
    this.openResourceAction(rowId)
  }

  setResourceActionRiskAcceptance(acceptRisk: boolean): void {
    const current = this.state.resourceAction
    if (current == null || current.loading || current.plan?.requiresRiskAcceptance !== true || this.resourceActionPosting) {
      return
    }
    this.state.resourceAction = { ...current, acceptRisk }
  }

  cancelResourceAction(): void {
    this.resourceActionEpoch++
    this.resourceActionAbort?.abort()
    this.resourceActionAbort = null
    this.resourceActionGeneration = null
    this.resourceActionKind = null
    this.resourceActionPosting = false
    this.state.resourceAction = null
  }

  dismissResourceAction(): void {
    const current = this.state.resourceAction
    if (this.disposed || this.resourceActionPosting || current == null) {
      return
    }
    this.reportTelemetry({ action: 'modal_outcome', modal: 'toggle', outcome: 'cancelled', rowId: current.rowId })
    this.cancelResourceAction()
  }

  async confirmResourceAction(): Promise<boolean> {
    const current = this.state.resourceAction
    const plan = current?.plan ?? null
    const generation = this.resourceActionGeneration
    if (
      this.disposed || current == null || plan == null || current.loading || this.resourceActionPosting ||
      this.state.operation != null || this.state.requesting || !eligible(this.state) || this.state.configuration?.editable !== true
    ) {
      return false
    }
    const row = this.state.rows.find((candidate) => candidate.id === current.rowId)
    if (generation == null || generation !== this.snapshotGeneration || plan.rowId !== current.rowId || plan.generation !== generation || row?.resourceAction !== plan.action || plan.action !== this.resourceActionKind) {
      this.state.resourceAction = { ...current, plan: null, error: RESOURCE_ACTION_STALE_MESSAGE, acceptRisk: false }
      return false
    }
    if (plan.requiresRiskAcceptance && !current.acceptRisk) {
      return false
    }

    const epoch = this.resourceActionEpoch
    const abort = new AbortController()
    this.resourceActionAbort = abort
    this.resourceActionPosting = true
    this.state.resourceAction = { ...current, loading: true, error: null }
    try {
      const operation = await this.client.resourceAction(current.rowId, plan.planId, current.acceptRisk, abort.signal)
      if (!this.resourceActionPostIsCurrent(current.rowId, epoch, abort.signal)) {
        return false
      }
      if (operation.id <= 0 || operation.kind !== 'mutation') {
        this.resourceActionAbort = null
        this.resourceActionPosting = false
        const latest = this.state.resourceAction
        if (latest != null) {
          this.state.resourceAction = { ...latest, loading: false, plan: null, error: RESOURCE_ACTION_FAILED_MESSAGE, acceptRisk: false }
        }
        return false
      }
      this.resourceActionAbort = null
      this.resourceActionPosting = false
      this.resourceActionEpoch++
      this.resourceActionGeneration = null
      this.resourceActionKind = null
      this.state.resourceAction = null
      const superseded = this.isSupersededOperation(operation)
      const activeOperation = this.state.operation as OperationRef | null
      if (!superseded && operation.id > (this.lastSettled?.id ?? 0) && (activeOperation == null || activeOperation.id <= operation.id)) {
        if (activeOperation?.id !== operation.id) {
          this.state.phase = null
        }
        this.state.operation = operation
      }
      if (superseded) {
        if ((this.state.settlementIntent?.operation.id ?? 0) <= operation.id) {
          this.state.settlementIntent = null
        }
        if ((this.state.settledMutation?.operation.id ?? 0) <= operation.id) {
          this.state.settledMutation = null
        }
      } else {
        if ((this.state.settlementIntent?.operation.id ?? 0) <= operation.id) {
          this.state.settlementIntent = {
            operation,
            rowId: current.rowId,
            selectNext: plan.action === 'remove' || plan.action === 'exclude',
          }
        }
        if ((this.state.settledMutation?.operation.id ?? 0) <= operation.id) {
          this.state.settledMutation = null
        }
      }
      this.tryCompleteResourceActionSettlement()
      return true
    } catch (error) {
      if (!this.resourceActionPostIsCurrent(current.rowId, epoch, abort.signal)) {
        return false
      }
      this.resourceActionAbort = null
      this.resourceActionPosting = false
      const latest = this.state.resourceAction
      if (latest != null) {
        this.state.resourceAction = { ...latest, loading: false, plan: null, error: resourceActionErrorMessage(error, false), acceptRisk: false }
      }
      return false
    }
  }

  consumeSettledMutation(operationId: number): void {
    if (this.state.settledMutation?.operation.id === operationId) {
      this.state.settledMutation = null
    }
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
        if (event.operation?.kind === 'mutation') {
          this.lastMutationNotification = { operation: event.operation, succeeded: event.payload.severity !== 'error' }
          this.tryCompleteResourceActionSettlement()
        }
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
      this.tryCompleteResourceActionSettlement()
      if (quitNow) {
        this.onQuit?.()
      }
      return
    }
    this.applySnapshotFields(snapshot, generation)
    this.tryCompleteResourceActionSettlement()
  }

  private applySnapshotFields(snapshot: Snapshot, generation: number): void {
    if (this.state.inspection != null && generation !== this.snapshotGeneration) {
      this.closeInspection()
    }
    if (this.state.resourceAction != null && generation !== this.resourceActionGeneration && !this.resourceActionPosting) {
      this.cancelResourceAction()
    }
    this.snapshotGeneration = generation
    if (this.accepted && snapshot.stage !== 'final') {
      return
    }
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
    this.cancelResourceActionIfRowChanged()
    if (snapshot.stage === 'final') {
      this.cancelInspectionIfRowMissing()
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
    this.cancelInspectionIfRowMissing()
    this.cancelResourceActionIfRowChanged()
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

  private issueInspection(rowId: string, fileId?: string): void {
    if (this.disposed) {
      return
    }
    const row = this.state.rows.find((candidate) => candidate.id === rowId)
    if (row == null || !canInspectResource(row)) {
      this.closeInspection()
      return
    }
    const previous = this.state.inspection
    const data = previous?.rowId === rowId ? previous.data : null
    const selectedFileId = fileId ?? (previous?.rowId === rowId ? previous.selectedFileId : null)
    this.inspectionEpoch++
    const epoch = this.inspectionEpoch
    this.inspectionAbort?.abort()
    const abort = new AbortController()
    const generation = this.snapshotGeneration
    this.inspectionAbort = abort
    this.inspectionGeneration = generation
    this.state.inspection = { rowId, loading: true, data, error: null, selectedFileId }
    const request = row.inspectable
      ? this.client.inspection(rowId, fileId, abort.signal)
      : this.client.installedSkillFiles(rowId, fileId, abort.signal)
    void request.then((payload) => {
      if (!this.inspectionRequestIsCurrent(rowId, generation, epoch, abort.signal)) {
        return
      }
      const current = this.state.inspection
      if (current == null) {
        return
      }
      if (
        payload.rowId !== rowId ||
        payload.generation !== generation ||
        payload.selectedFileId !== (fileId ?? payload.primaryFileId) ||
        !payload.files.some((file) => file.id === payload.primaryFileId) ||
        !payload.files.some((file) => file.id === payload.selectedFileId)
      ) {
        this.inspectionAbort = null
        this.state.inspection = { ...current, loading: false, error: INSPECTION_UNAVAILABLE_MESSAGE }
        return
      }
      this.inspectionAbort = null
      this.state.inspection = { ...current, loading: false, data: payload, error: null, selectedFileId: payload.selectedFileId }
    }).catch((error: unknown) => {
      if (!this.inspectionRequestIsCurrent(rowId, generation, epoch, abort.signal)) {
        return
      }
      this.inspectionAbort = null
      const current = this.state.inspection
      if (current != null) {
        const message = error instanceof RequestError && error.code === 'localapi.resource_files_unavailable'
          ? RESOURCE_FILES_UNAVAILABLE_MESSAGE
          : INSPECTION_UNAVAILABLE_MESSAGE
        this.state.inspection = { ...current, loading: false, error: message }
      }
    })
  }

  private inspectionRequestIsCurrent(rowId: string, generation: number, epoch: number, signal: AbortSignal): boolean {
    if (
      this.disposed ||
      signal.aborted ||
      epoch !== this.inspectionEpoch ||
      generation !== this.snapshotGeneration ||
      generation !== this.inspectionGeneration ||
      this.state.inspection?.rowId !== rowId
    ) {
      return false
    }
    const row = this.state.rows.find((candidate) => candidate.id === rowId)
    if (row == null || !canInspectResource(row)) {
      this.closeInspection()
      return false
    }
    return true
  }

  private cancelInspectionIfRowMissing(): void {
    const current = this.state.inspection
    if (current != null && !this.state.rows.some((row) => row.id === current.rowId && canInspectResource(row))) {
      this.closeInspection()
    }
  }

  private resourceActionPostIsCurrent(rowId: string, epoch: number, signal: AbortSignal): boolean {
    return !this.disposed && !signal.aborted && epoch === this.resourceActionEpoch && this.state.resourceAction?.rowId === rowId
  }

  private isSupersededOperation(operation: OperationRef): boolean {
    const activeOperation = this.state.operation as OperationRef | null
    return (activeOperation != null && activeOperation.id > operation.id) ||
      (this.lastSettled != null && this.lastSettled.id > operation.id)
  }

  private resourceActionRequestIsCurrent(rowId: string, generation: number, epoch: number, signal: AbortSignal): boolean {
    if (
      this.disposed || signal.aborted || epoch !== this.resourceActionEpoch || generation !== this.snapshotGeneration ||
      generation !== this.resourceActionGeneration || this.state.resourceAction?.rowId !== rowId
    ) {
      return false
    }
    const row = this.state.rows.find((candidate) => candidate.id === rowId)
    if (row == null || row.resourceAction !== this.resourceActionKind) {
      this.cancelResourceAction()
      return false
    }
    return true
  }

  private cancelResourceActionIfRowChanged(): void {
    if (this.resourceActionPosting) {
      return
    }
    const current = this.state.resourceAction
    if (current == null) {
      return
    }
    const row = this.state.rows.find((candidate) => candidate.id === current.rowId)
    if (row == null || row.resourceAction !== this.resourceActionKind) {
      this.cancelResourceAction()
    }
  }

  private tryCompleteResourceActionSettlement(): void {
    const intent = this.state.settlementIntent
    if (intent == null) {
      return
    }
    if (this.isSupersededOperation(intent.operation)) {
      if (this.state.settlementIntent?.operation.id === intent.operation.id) {
        this.state.settlementIntent = null
      }
      if (this.state.settledMutation?.operation.id === intent.operation.id) {
        this.state.settledMutation = null
      }
      return
    }
    const notification = this.lastMutationNotification
    if (this.lastSettled?.id !== intent.operation.id || notification?.operation.id !== intent.operation.id) {
      return
    }
    this.state.settlementIntent = null
    if (notification.succeeded && (this.state.settledMutation == null || this.state.settledMutation.operation.id <= intent.operation.id)) {
      this.state.settledMutation = intent
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

function resourceActionErrorMessage(error: unknown, preparing: boolean): string {
  if (error instanceof RequestError) {
    if (error.status === 409 || error.code === 'localapi.busy') {
      return RESOURCE_ACTION_BUSY_MESSAGE
    }
    if (error.code === 'localapi.not_ready' || error.code === 'localapi.not_supported' || error.code === 'localapi.invalid_request') {
      return RESOURCE_ACTION_STALE_MESSAGE
    }
  }
  return preparing ? RESOURCE_ACTION_UNAVAILABLE_MESSAGE : RESOURCE_ACTION_FAILED_MESSAGE
}
