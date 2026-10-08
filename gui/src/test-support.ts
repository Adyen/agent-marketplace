import { afterEach, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ui from '@nuxt/ui/vue-plugin'
import WorkspaceView from './WorkspaceView.vue'
import {
  EventCursor,
  type Client,
  type Configuration,
  type OperationRef,
  type SessionEvent,
  type Snapshot,
} from './api/client'
import { WorkspaceController, type Row } from './controller'

export function snapshot(rows: Row[] = [], overrides: Partial<Snapshot> = {}): Snapshot {
  return {
    stage: 'final',
    drift: 'ready',
    rows,
    diagnostics: [],
    harnesses: ['devin'],
    operation: null,
    ...overrides,
  }
}

export function event(overrides: Partial<SessionEvent>): SessionEvent {
  return { type: 'snapshot', sequence: 0, generation: 1, operation: null, payload: snapshot(), ...overrides } as SessionEvent
}

export function configuration(overrides: Partial<Configuration> = {}): Configuration {
  return {
    userTargets: ['devin'],
    organizationTargets: [],
    supportedTargets: ['devin'],
    rootPresent: true,
    editable: true,
    managedOutsideGUI: [],
    firstRun: false,
    canInitialize: false,
    detectedTargets: [],
    detectionUnavailable: false,
    consentRequired: false,
    telemetryDestination: 'not configured',
    retentionPolicyURL: 'not configured',
    ...overrides,
  }
}

export function fakeClient(options: { configuration?: Partial<Configuration> } = {}) {
  const queue: SessionEvent[] = []
  let pending: ((value: IteratorResult<SessionEvent>) => void) | null = null
  const client: Client = {
    snapshot: vi.fn(async () => snapshot()),
    events: (cursor: EventCursor = new EventCursor()) => ({
      async *[Symbol.asyncIterator]() {
        for (;;) {
          let next: IteratorResult<SessionEvent>
          if (queue.length > 0) {
            next = { done: false, value: queue.shift()! }
          } else {
            next = await new Promise<IteratorResult<SessionEvent>>((resolve) => {
              pending = resolve
            })
          }
          if (next.done) {
            return
          }
          if (cursor.accept(next.value)) {
            yield next.value
          }
        }
      },
    }),
    action: vi.fn(async () => ({ id: 7, kind: 'sync' }) as OperationRef),
    configuration: vi.fn(async () => configuration(options.configuration)),
    configure: vi.fn(async () => ({ id: 8, kind: 'targets' }) as OperationRef),
  }
  const push = (e: SessionEvent) => {
    if (pending) {
      const resolve = pending
      pending = null
      resolve({ done: false, value: e })
    } else {
      queue.push(e)
    }
  }
  const stop = () => {
    queue.length = 0
    if (pending) {
      const resolve = pending
      pending = null
      resolve({ done: true, value: undefined })
    }
  }
  return { client, push, stop }
}

const cleanups: (() => void)[] = []

afterEach(() => {
  while (cleanups.length > 0) {
    cleanups.pop()!()
  }
  vi.useRealTimers()
})

export async function tick() {
  await flushPromises()
  await nextTick()
  if (vi.isFakeTimers()) {
    await vi.advanceTimersByTimeAsync(0)
  } else {
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
  await flushPromises()
}

export function mountWorkspace(client: Client, notifyTtl = 60_000) {
  const controller = new WorkspaceController(client, notifyTtl)
  const wrapper = mount(WorkspaceView, {
    attachTo: document.body,
    props: { controller },
    global: { plugins: [ui] },
  })
  void controller.connect()
  cleanups.push(() => {
    controller.dispose()
    wrapper.unmount()
    document.body.innerHTML = ''
  })
  return { controller, wrapper }
}
