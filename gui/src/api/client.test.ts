import { describe, expect, it } from 'vitest'
import { createClient, EventCursor, RequestError } from './client'
import type { SessionEvent, Snapshot } from './client'

function snapshotEvent(sequence: number, generation: number, stage = 'final', operation: number | null = null): SessionEvent {
  const ref = operation == null ? null : { id: operation, kind: 'sync' as const }
  return {
    type: 'snapshot',
    sequence,
    generation,
    operation: ref,
    payload: {
      stage: stage as Snapshot['stage'],
      drift: 'ready',
      rows: [
        {
          id: `skill/gen-${generation}`,
          name: `gen-${generation}`,
          kind: 'skill',
          description: '',
          registry: 'local',
          scope: 'project',
          required: false,
          pendingVerification: false,
          checked: true,
          harnessOverride: false,
          harnesses: ['devin'],
          status: 'enabled',
        },
      ],
      diagnostics: [],
      harnesses: ['devin'],
      operation: ref,
      pendingChanges: 0,
      migrationOffered: false,
    },
  }
}

function progressEvent(sequence: number, generation: number, operation: number, progressSequence: number, phase = 'materialize'): SessionEvent {
  return {
    type: 'progress',
    sequence,
    generation,
    operation: { id: operation, kind: 'sync' },
    payload: { progressSequence, phase },
  }
}

function notificationEvent(sequence: number, generation: number, operation: number): SessionEvent {
  return {
    type: 'notification',
    sequence,
    generation,
    operation: { id: operation, kind: 'sync' },
    payload: { severity: 'info', message: 'done' },
  }
}

describe('EventCursor', () => {
  it('rejects duplicate and out-of-order stream sequences', () => {
    const cursor = new EventCursor()
    expect(cursor.accept(snapshotEvent(1, 1))).toBe(true)
    expect(cursor.accept(snapshotEvent(1, 1))).toBe(false)
    expect(cursor.accept(snapshotEvent(2, 1))).toBe(true)
    expect(cursor.accept(snapshotEvent(2, 1))).toBe(false)
  })

  it('rejects an older snapshot generation without replacing newer state', () => {
    const cursor = new EventCursor()
    expect(cursor.accept(snapshotEvent(4, 3))).toBe(true)
    expect(cursor.accept(snapshotEvent(5, 2))).toBe(false)
    expect(cursor.accept(snapshotEvent(6, 3))).toBe(true)
  })

  it('rejects a stage regression inside one generation', () => {
    const cursor = new EventCursor()
    expect(cursor.accept(snapshotEvent(1, 3, 'final'))).toBe(true)
    expect(cursor.accept(snapshotEvent(2, 3, 'inventory'))).toBe(false)
    expect(cursor.accept(snapshotEvent(3, 3, 'final'))).toBe(true)
  })

  it('accepts progress only for the active operation of the current generation', () => {
    const cursor = new EventCursor()
    expect(cursor.accept(progressEvent(1, 1, 7, 1))).toBe(false)
    expect(cursor.accept(snapshotEvent(2, 2, 'final', 7))).toBe(true)
    expect(cursor.accept(progressEvent(3, 2, 7, 1))).toBe(true)
    expect(cursor.accept(progressEvent(4, 2, 7, 1))).toBe(false)
    expect(cursor.accept(progressEvent(5, 2, 9, 2))).toBe(false)
    expect(cursor.accept(progressEvent(6, 1, 7, 2))).toBe(false)
    expect(cursor.accept(progressEvent(7, 2, 7, 2))).toBe(true)
  })

  it('rejects progress after the final snapshot cleared the operation', () => {
    const cursor = new EventCursor()
    expect(cursor.accept(snapshotEvent(1, 2, 'final', 7))).toBe(true)
    expect(cursor.accept(snapshotEvent(2, 2, 'final'))).toBe(true)
    expect(cursor.accept(progressEvent(3, 2, 7, 5))).toBe(false)
  })

  it('rejects progress for a finished operation so it cannot revive it', () => {
    const cursor = new EventCursor()
    expect(cursor.accept(snapshotEvent(1, 1, 'final', 7))).toBe(true)
    expect(cursor.accept(progressEvent(2, 1, 7, 1))).toBe(true)
    expect(cursor.accept(notificationEvent(3, 1, 7))).toBe(true)
    expect(cursor.accept(progressEvent(4, 1, 7, 2))).toBe(false)
  })

  it('rejects an older-generation notification', () => {
    const cursor = new EventCursor()
    expect(cursor.accept(snapshotEvent(1, 3))).toBe(true)
    expect(cursor.accept(notificationEvent(2, 1, 7))).toBe(false)
  })

  it('rejects progress with no operation identity', () => {
    const cursor = new EventCursor()
    expect(cursor.accept(snapshotEvent(1, 1, 'final', 7))).toBe(true)
    const event = { ...progressEvent(2, 1, 7, 1), operation: null } as SessionEvent
    expect(cursor.accept(event)).toBe(false)
  })
})

describe('createClient', () => {
  it('sends the credential header on POST and parses snapshot JSON', async () => {
    const calls: { url: string; method: string; token: string | null }[] = []
    const event = snapshotEvent(1, 1)
    const payload = event.type === 'snapshot' ? event.payload : null
    const fetcher: typeof fetch = async (input, init) => {
      calls.push({
        url: String(input),
        method: init?.method ?? '',
        token: new Headers(init?.headers).get('X-Agent-Marketplace-Token'),
      })
      return new Response(JSON.stringify(payload), { status: 200 })
    }
    const client = createClient('http://127.0.0.1:9999/', 'secret-token', fetcher)
    const snapshot = await client.snapshot()
    expect(snapshot.rows[0]?.name).toBe('gen-1')
    expect(calls).toEqual([
      {
        url: 'http://127.0.0.1:9999/api/snapshot',
        method: 'POST',
        token: 'secret-token',
      },
    ])
  })

  it('parses fragmented SSE bytes into ordered events', async () => {
    const events = [snapshotEvent(1, 1, 'final', 5), progressEvent(2, 1, 5, 1), notificationEvent(3, 1, 5)]
    const raw = events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join('')
    const chunks = [raw.slice(0, 10), raw.slice(10, 25), raw.slice(25)]
    const fetcher: typeof fetch = async () =>
      new Response(
        new ReadableStream<Uint8Array>({
          start(controller) {
            for (const chunk of chunks) {
              controller.enqueue(new TextEncoder().encode(chunk))
            }
            controller.close()
          },
        }),
        { status: 200 },
      )
    const client = createClient('http://127.0.0.1:9999', 'secret-token', fetcher)
    const received: SessionEvent[] = []
    for await (const event of client.events()) {
      received.push(event)
    }
    expect(received.map((event) => event.sequence)).toEqual([1, 2, 3])
  })

  it('filters out-of-order events inside a single stream', async () => {
    const events = [snapshotEvent(1, 2), snapshotEvent(3, 1), progressEvent(2, 2, 5, 1)]
    const raw = events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join('')
    const fetcher: typeof fetch = async () =>
      new Response(
        new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(new TextEncoder().encode(raw))
            controller.close()
          },
        }),
        { status: 200 },
      )
    const client = createClient('http://127.0.0.1:9999', 'secret-token', fetcher)
    const received: SessionEvent[] = []
    for await (const event of client.events()) {
      received.push(event)
    }
    expect(received).toHaveLength(1)
    expect(received[0].sequence).toBe(1)
  })

  it('issues root-relative /api requests for an empty native base URL', async () => {
    const calls: string[] = []
    const event = snapshotEvent(1, 1)
    const fetcher: typeof fetch = async (input) => {
      calls.push(String(input))
      return new Response(
        new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(event)}\n\n`))
            controller.close()
          },
        }),
        { status: 200 },
      )
    }
    const client = createClient('', 'secret-token', fetcher)
    const received: SessionEvent[] = []
    for await (const event of client.events()) {
      received.push(event)
    }
    expect(calls).toEqual(['/api/events'])
    expect(received).toHaveLength(1)

    calls.length = 0
    const jsonFetcher: typeof fetch = async (input) => {
      calls.push(String(input))
      return new Response(JSON.stringify(event.type === 'snapshot' ? event.payload : {}), { status: 200 })
    }
    await createClient('', 'secret-token', jsonFetcher).snapshot()
    expect(calls).toEqual(['/api/snapshot'])
  })

  it('reads configuration metadata with no body', async () => {
    const calls: { url: string; body: string | null }[] = []
    const metadata = {
      userTargets: ['devin', 'future-harness'],
      organizationTargets: ['claude'],
      supportedTargets: ['claude', 'devin'],
      rootPresent: true,
      editable: true,
      managedOutsideGUI: ['other settings'],
    }
    const fetcher: typeof fetch = async (input, init) => {
      calls.push({ url: String(input), body: init?.body == null ? null : String(init.body) })
      return new Response(JSON.stringify(metadata), { status: 200 })
    }
    const client = createClient('http://127.0.0.1:9999', 'secret-token', fetcher)
    const result = await client.configuration()
    expect(result.userTargets).toEqual(['devin', 'future-harness'])
    expect(result.supportedTargets).toEqual(['claude', 'devin'])
    expect(calls).toEqual([{ url: 'http://127.0.0.1:9999/api/configuration', body: null }])
  })

  it('posts the typed save-targets action and returns the operation ref', async () => {
    const calls: { url: string; body: string | null }[] = []
    const fetcher: typeof fetch = async (input, init) => {
      calls.push({ url: String(input), body: init?.body == null ? null : String(init.body) })
      return new Response(JSON.stringify({ operation: { id: 4, kind: 'targets' } }), { status: 202 })
    }
    const client = createClient('http://127.0.0.1:9999', 'secret-token', fetcher)
    const operation = await client.configure({ action: 'save-targets', targets: ['devin', 'future-harness'] })
    expect(operation).toEqual({ id: 4, kind: 'targets' })
    expect(calls).toEqual([
      {
        url: 'http://127.0.0.1:9999/api/configuration/actions',
        body: '{"action":"save-targets","targets":["devin","future-harness"]}',
      },
    ])
  })

  // spec: gui-shell/gui-actions-use-existing-telemetry-policy
  it('posts a closed telemetry event and accepts the empty response', async () => {
    const calls: { url: string; method: string; token: string | null; body: string | null }[] = []
    const fetcher: typeof fetch = async (input, init) => {
      calls.push({
        url: String(input),
        method: init?.method ?? '',
        token: new Headers(init?.headers).get('X-Agent-Marketplace-Token'),
        body: init?.body == null ? null : String(init.body),
      })
      return new Response(null, { status: 204 })
    }
    const client = createClient('http://127.0.0.1:9999', 'secret-token', fetcher)
    await client.telemetry({ action: 'browse', rowId: 'opaque-row-id' })
    expect(calls).toEqual([
      {
        url: 'http://127.0.0.1:9999/api/telemetry',
        method: 'POST',
        token: 'secret-token',
        body: '{"action":"browse","rowId":"opaque-row-id"}',
      },
    ])
  })

  it('raises RequestError with the server code on rejection', async () => {
    const fetcher: typeof fetch = async () =>
      new Response(JSON.stringify({ code: 'localapi.busy', message: 'busy' }), { status: 409 })
    const client = createClient('http://127.0.0.1:9999', 'secret-token', fetcher)
    await expect(client.action('sync')).rejects.toMatchObject({
      name: 'RequestError',
      status: 409,
      code: 'localapi.busy',
    } satisfies Partial<RequestError>)
  })
})
