import { test, expect } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { resolve, join } from 'node:path'
import ts from 'typescript'
import { guiRoot, startHarness } from './harness'

const fixtureSkillBody = '---\nname: solo\ndescription: Solo skill\n---\n# Solo\n'

async function clientModule(): Promise<string> {
  const source = await readFile(resolve(guiRoot, 'src', 'api', 'client.ts'), 'utf8')
  const transpiled = ts.transpileModule(source.replace(/^import[^;]+;\n/gm, ''), {
    compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2022 },
  })
  return transpiled.outputText
}

test('real core over authenticated loopback', async ({ page, request }) => {
  const harness = await startHarness('workspace')
  const { bootstrap } = harness
  const repo = bootstrap.repo!
  const configPath = join(repo, '.agents', 'config.yaml')
  const configBefore = await readFile(configPath)
  const targetPath = join(repo, '.devin', 'skills', 'solo', 'SKILL.md')
  try {
    await page.goto(`${bootstrap.url}/does-not-exist`, { waitUntil: 'domcontentloaded' }).catch(() => undefined)

    const moduleSource = await clientModule()
    const result = await page.evaluate(
      async ({ source, url, credential }) => {
        const client = await import(`data:text/javascript;base64,${btoa(source)}`)
        const api = client.createClient(url, credential)
        const snapshot = await api.snapshot()
        const rowNames = snapshot.rows.map((row: { name: string }) => row.name)

        const observeAction = async (action: string) => {
          const events = api.events()
          const iterator = events[Symbol.asyncIterator]()
          const envelope = await iterator.next()
          const operation = await api.action(action)
          const progressOps: number[] = []
          let sawNotification = false
          let sawActiveSnapshot = false
          let finalCleared = false
          let severity = ''
          const deadline = Date.now() + 60_000
          while (!sawNotification && Date.now() < deadline) {
            const next = await iterator.next()
            if (next.done) break
            const event = next.value
            if (event.type === 'progress') {
              progressOps.push(event.operation?.id ?? -1)
            }
            if (event.type === 'snapshot') {
              if (event.operation?.id === operation.id) {
                sawActiveSnapshot = true
              }
              if (event.operation == null && progressOps.length > 0) {
                finalCleared = true
              }
            }
            if (event.type === 'notification') {
              sawNotification = true
              severity = event.payload.severity
            }
          }
          await iterator.return?.()
          return { envelope: envelope.value, operation, progressOps, sawActiveSnapshot, finalCleared, sawNotification, severity }
        }

        const unauthed = client.createClient(url, 'not-the-token')
        const denied = await unauthed.action('sync').then(
          () => 'accepted',
          (error: { status?: number }) => `rejected:${error.status}`,
        )

        const sync = await observeAction('sync')
        const update = await observeAction('update')

        const cursor = new client.EventCursor()
        const accepted: boolean[] = [
          cursor.accept({ type: 'snapshot', sequence: 2, generation: 2, operation: { id: 9, kind: 'sync' }, payload: snapshot }),
          cursor.accept({ type: 'snapshot', sequence: 2, generation: 2, operation: { id: 9, kind: 'sync' }, payload: snapshot }),
          cursor.accept({ type: 'snapshot', sequence: 3, generation: 1, operation: null, payload: snapshot }),
          cursor.accept({ type: 'progress', sequence: 4, generation: 2, operation: { id: 9, kind: 'sync' }, payload: { progressSequence: 1, phase: 'x' } }),
          cursor.accept({ type: 'progress', sequence: 5, generation: 2, operation: { id: 8, kind: 'sync' }, payload: { progressSequence: 1, phase: 'x' } }),
          cursor.accept({ type: 'notification', sequence: 6, generation: 2, operation: { id: 9, kind: 'sync' }, payload: { severity: 'info', message: 'done' } }),
          cursor.accept({ type: 'progress', sequence: 7, generation: 2, operation: { id: 9, kind: 'sync' }, payload: { progressSequence: 2, phase: 'x' } }),
        ]

        return { rowNames, denied, sync, update, accepted, snapshotStage: snapshot.stage }
      },
      { source: moduleSource, url: bootstrap.url, credential: bootstrap.credential },
    )

    expect(result.snapshotStage).toBe('final')
    expect(result.rowNames).toContain('solo')
    expect(result.denied).toBe('rejected:401')
    expect(result.accepted).toEqual([true, false, false, true, false, true, false])
    for (const [run, kind] of [[result.sync, 'sync'], [result.update, 'update']] as const) {
      expect(run.envelope?.type).toBe('snapshot')
      expect(run.operation.kind).toBe(kind)
      expect(run.sawNotification).toBe(true)
      expect(run.severity).toBe('info')
      expect(run.sawActiveSnapshot).toBe(true)
      expect(run.finalCleared).toBe(true)
      expect(run.progressOps.length).toBeGreaterThan(0)
      for (const id of run.progressOps) {
        expect(id).toBe(run.operation.id)
      }
    }

    const configAfterActions = await readFile(configPath)
    expect(configAfterActions.equals(configBefore)).toBe(true)
    const materializedAfterActions = await readFile(targetPath, 'utf8')
    expect(materializedAfterActions).toBe(fixtureSkillBody)

    const foreign = await request.post(`${bootstrap.url}/api/actions`, {
      headers: {
        'X-Agent-Marketplace-Token': bootstrap.credential,
        Origin: 'https://evil.example',
        'Content-Type': 'application/json',
      },
      data: { action: 'sync' },
    })
    expect(foreign.status()).toBe(403)
    expect((await foreign.json()).code).toBe('localapi.forbidden')

    const configAfter = await readFile(configPath)
    expect(configAfter.equals(configAfterActions)).toBe(true)
    const materialized = await readFile(targetPath, 'utf8')
    expect(materialized).toBe(materializedAfterActions)
  } finally {
    await harness.stop()
  }
})
