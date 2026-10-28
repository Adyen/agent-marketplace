import { afterEach, vi } from 'vitest'
import { flushPromises, mount, type DOMWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import ui from '@nuxt/ui/vue-plugin'
import WorkspaceView from './WorkspaceView.vue'
import {
  EventCursor,
  type Client,
  type Configuration,
  type InspectionPayload,
  type InstalledSkillFilesPayload,
  type OperationRef,
  type SessionEvent,
} from './api/client'
import { configuration, snapshot } from './fixtures'
import { WorkspaceController } from './controller'

export { configuration, event, row, snapshot } from './fixtures'

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
    release: vi.fn(async () => ({ latestVersion: '' })),
    telemetry: vi.fn(async () => undefined),
    discovery: vi.fn(async () => undefined),
    inspection: vi.fn(async (): Promise<InspectionPayload> => {
      throw new Error('inspection fixture response not configured')
    }),
    installedSkillFiles: vi.fn(async (): Promise<InstalledSkillFilesPayload> => {
      throw new Error('installed skill files fixture response not configured')
    }),
    resourcePlan: vi.fn(async () => {
      throw new Error('resource action plan fixture response not configured')
    }),
    resourceAction: vi.fn(async () => {
      throw new Error('resource action fixture response not configured')
    }),
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

export function mountWorkspace(client: Client, notifyTtl = 60_000, onQuit?: () => void) {
  const controller = new WorkspaceController(client, notifyTtl, onQuit)
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

export function rowNames(wrapper: { findAll(selector: string): DOMWrapper<Element>[] }): string[] {
  return wrapper.findAll('.workspace__name').map((name) => name.text())
}
