import type { components, paths } from '../generated/api'

export type Snapshot = components['schemas']['Snapshot']
export type SnapshotRow = components['schemas']['SnapshotRow']
export type DiscoveryHealth = components['schemas']['DiscoveryHealth']
export type DiscoveryPayload = components['schemas']['DiscoveryEvent']['payload']
export type DiscoveryEvent = components['schemas']['DiscoveryEvent']
export type OperationRef = components['schemas']['OperationRef']
export type SessionEvent = components['schemas']['SessionEvent']
export type DiscoveryRequest = components['schemas']['DiscoveryRequest']
export type Action = components['schemas']['ActionRequest']['action']
export type Configuration = components['schemas']['Configuration']
export type SourceDescription = components['schemas']['SourceDescription']
export type SourceInput = components['schemas']['SourceInput']
export type ConfigureRequest = components['schemas']['ConfigurationActionRequest']
export type ApiError = components['schemas']['Error']
export type ReleaseNotice = components['schemas']['ReleaseNotice']
export type GUIEvent = components['schemas']['GUIEvent']
export type InspectionRequest = components['schemas']['InspectionRequest']
export type InspectionPayload = components['schemas']['InspectionPayload']
export type InstalledSkillFilesRequest = components['schemas']['InstalledSkillFilesRequest']
export type InstalledSkillFilesPayload = components['schemas']['InstalledSkillFilesPayload']
export type InspectionFile = components['schemas']['InspectionFile']

export class RequestError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
  ) {
    super(`request failed: ${status} ${code}`)
    this.name = 'RequestError'
  }
}

const stageRank: Record<string, number> = {
  provisional: 1,
  config: 2,
  inventory: 3,
  final: 4,
}

export class EventCursor {
  private sequence = 0
  private generation = 0
  private stage = 0
  private operation = 0
  private progressSequence = 0
  private discoverySequence = 0

  accept(event: SessionEvent): boolean {
    if (typeof event.sequence !== 'number' || event.sequence <= this.sequence) {
      return false
    }
    this.sequence = event.sequence
    switch (event.type) {
      case 'snapshot': {
        const generation = event.generation ?? 0
        if (generation < this.generation) {
          return false
        }
        const rank = stageRank[event.payload.stage] ?? 0
        if (generation === this.generation && rank < this.stage) {
          return false
        }
        if (generation > this.generation) {
          this.generation = generation
          this.stage = 0
          this.operation = 0
          this.progressSequence = 0
        }
        this.stage = rank
        const nextOperation = event.operation?.id ?? 0
        if (nextOperation !== this.operation) {
          this.progressSequence = 0
        }
        this.operation = nextOperation
        const discoverySequence = event.payload.discoverySequence
        if (typeof discoverySequence === 'number' && discoverySequence > this.discoverySequence) {
          this.discoverySequence = discoverySequence
        }
        return true
      }
      case 'progress': {
        const operation = event.operation
        const next = event.payload.progressSequence
        if (
          operation == null ||
          event.generation !== this.generation ||
          this.operation === 0 ||
          operation.id !== this.operation ||
          typeof next !== 'number' ||
          next <= this.progressSequence
        ) {
          return false
        }
        this.progressSequence = next
        return true
      }
      case 'discovery': {
        const next = event.payload.discoverySequence
        if (
          event.operation !== null ||
          event.generation !== this.generation ||
          typeof next !== 'number' ||
          next <= this.discoverySequence
        ) {
          return false
        }
        this.discoverySequence = next
        return true
      }
      case 'notification': {
        const generation = event.generation ?? 0
        if (generation < this.generation) {
          return false
        }
        return true
      }
      default:
        return false
    }
  }
}

function parseEvents(buffer: string): { events: SessionEvent[]; rest: string } {
  const events: SessionEvent[] = []
  let rest = buffer
  for (;;) {
    const boundary = rest.indexOf('\n\n')
    const boundaryCR = rest.indexOf('\r\n\r\n')
    let end = -1
    let skip = 0
    if (boundary !== -1 && (boundaryCR === -1 || boundary < boundaryCR)) {
      end = boundary
      skip = 2
    } else if (boundaryCR !== -1) {
      end = boundaryCR
      skip = 4
    }
    if (end === -1) {
      break
    }
    const block = rest.slice(0, end)
    rest = rest.slice(end + skip)
    const data = block
      .split(/\r?\n/)
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).replace(/^ /, ''))
      .join('\n')
    if (data !== '') {
      events.push(JSON.parse(data) as SessionEvent)
    }
  }
  return { events, rest }
}

export interface Client {
  snapshot(): Promise<Snapshot>
  events(cursor?: EventCursor, signal?: AbortSignal): AsyncIterable<SessionEvent>
  discovery(query: string, requestId: number, signal?: AbortSignal): Promise<void>
  inspection(rowId: string, fileId?: string, signal?: AbortSignal): Promise<InspectionPayload>
  installedSkillFiles(rowId: string, fileId?: string, signal?: AbortSignal): Promise<InstalledSkillFilesPayload>
  action(action: Action): Promise<OperationRef>
  configuration(): Promise<Configuration>
  configure(request: ConfigureRequest): Promise<OperationRef>
  release(): Promise<ReleaseNotice>
  telemetry(event: GUIEvent): Promise<void>
}

export function createClient(baseURL: string, credential: string, fetchImpl: typeof fetch = fetch): Client {
  const root = baseURL.replace(/\/+$/, '')
  const headers = { 'X-Agent-Marketplace-Token': credential }

  async function post(path: string, body?: unknown, signal?: AbortSignal): Promise<Response> {
    const response = await fetchImpl(`${root}${path}`, {
      method: 'POST',
      signal,
      headers:
        body === undefined
          ? headers
          : { ...headers, 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    if (!response.ok) {
      let code = ''
      try {
        code = ((await response.json()) as ApiError).code ?? ''
      } catch {

      }
      throw new RequestError(response.status, code)
    }
    return response
  }

  return {
    async snapshot() {
      const response = await post('/api/snapshot')
      return (await response.json()) as Snapshot
    },

    async *events(cursor: EventCursor = new EventCursor(), signal?: AbortSignal) {
      const response = await post('/api/events', undefined, signal)
      if (response.body == null) {
        throw new RequestError(response.status, 'localapi.no_stream')
      }
      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      try {
        for (;;) {
          const { done, value } = await reader.read()
          if (done) {
            break
          }
          buffer += decoder.decode(value, { stream: true })
          const parsed = parseEvents(buffer)
          buffer = parsed.rest
          for (const event of parsed.events) {
            if (cursor.accept(event)) {
              yield event
            }
          }
        }
      } finally {
        reader.cancel().catch(() => undefined)
      }
    },

    async discovery(query: string, requestId: number, signal?: AbortSignal) {
      await post(
        '/api/discovery',
        { query, requestId } satisfies NonNullable<paths['/api/discovery']['post']['requestBody']>['content']['application/json'],
        signal,
      )
    },

    async inspection(rowId: string, fileId?: string, signal?: AbortSignal) {
      const request = { rowId, ...(fileId === undefined ? {} : { fileId }) } satisfies InspectionRequest
      const response = await post('/api/inspection', request, signal)
      return (await response.json()) as InspectionPayload
    },

    async installedSkillFiles(rowId: string, fileId?: string, signal?: AbortSignal) {
      const request = { rowId, ...(fileId === undefined ? {} : { fileId }) } satisfies InstalledSkillFilesRequest
      const response = await post('/api/installed-skill-files', request, signal)
      return (await response.json()) as InstalledSkillFilesPayload
    },

    async action(action: Action) {
      const response = await post('/api/actions', { action } satisfies NonNullable<paths['/api/actions']['post']['requestBody']>['content']['application/json'])
      return ((await response.json()) as components['schemas']['ActionAccepted']).operation
    },

    async configuration() {
      const response = await post('/api/configuration')
      return (await response.json()) as Configuration
    },

    async configure(request: ConfigureRequest) {
      const response = await post('/api/configuration/actions', request satisfies NonNullable<paths['/api/configuration/actions']['post']['requestBody']>['content']['application/json'])
      return ((await response.json()) as components['schemas']['ActionAccepted']).operation
    },

    async release() {
      const response = await post('/api/release')
      return (await response.json()) as ReleaseNotice
    },

    async telemetry(event: GUIEvent) {
      await post('/api/telemetry', event satisfies NonNullable<paths['/api/telemetry']['post']['requestBody']>['content']['application/json'])
    },
  }
}
