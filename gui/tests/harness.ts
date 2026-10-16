import { spawn, type ChildProcess } from 'node:child_process'
import { createInterface } from 'node:readline'
import { resolve, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { writeFileSync } from 'node:fs'

export const guiRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
export const repoRoot = resolve(guiRoot, '..')

export interface Bootstrap {
  url: string
  credential: string
  repo?: string
  home?: string
  codexHome?: string
  xdgConfig?: string
  xdgState?: string
  xdgCache?: string
  addSourceDir?: string
  navigationSourceDir?: string
  orgConfig?: string
  marketplaceEndpoint?: string
  gitSourceURL?: string
  syncStarted?: string
  syncRelease?: string
  discoveryAcquisitionStarted?: string
  discoveryRelease?: string
  discoverySearchCount?: string
  discoveryFirstSearchStarted?: string
  discoveryFirstSearchCanceled?: string
  discoverySecondSearchStarted?: string
}

export interface Harness {
  bootstrap: Bootstrap
  stop: () => Promise<void>
}

export type HarnessMode = 'workspace' | 'user'

export async function startHarness(mode: HarnessMode, extraEnv: Record<string, string> = {}): Promise<Harness> {
  const child: ChildProcess = spawn(
    'go',
    [
      'test',
      './internal/localapi',
      '-run',
      mode === 'user' ? '^TestLocalAPIUserHarness$' : '^TestLocalAPIHarness$',
      '-count=1',
    ],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        AM_LOCALAPI_E2E: mode === 'user' ? '' : '1',
        AM_LOCALAPI_USER_E2E: mode === 'user' ? '1' : '',
        ...extraEnv,
        CGO_ENABLED: '0',
      },
      stdio: ['ignore', 'pipe', 'pipe', 'pipe'],
      detached: true,
    },
  )
  const stderrChunks: Buffer[] = []
  child.stderr?.on('data', (chunk: Buffer) => stderrChunks.push(chunk))
  child.stdout?.on('data', (chunk: Buffer) => stderrChunks.push(chunk))
  const pipe = child.stdio[3]
  if (pipe == null) {
    try {
      process.kill(-child.pid!, 'SIGKILL')
    } catch {
      child.kill('SIGKILL')
    }
    throw new Error('bootstrap fd 3 pipe missing')
  }
  const lines = createInterface({ input: pipe as NodeJS.ReadableStream })
  let exited = false
  let exitCode: number | null = null
  let exitSignal: NodeJS.Signals | null = null
  const exitPromise = new Promise<void>((resolvePromise) =>
    child.once('exit', (code, signal) => {
      exited = true
      exitCode = code
      exitSignal = signal
      resolvePromise()
    }),
  )
  let closed = false
  const closePromise = new Promise<void>((resolvePromise) =>
    child.once('close', () => {
      closed = true
      resolvePromise()
    }),
  )
  const stderrText = () => Buffer.concat(stderrChunks).toString('utf8')
  const dumpStderr = () => {
    const text = stderrText()
    if (text.trim() !== '') {
      const directory = extraEnv.AM_WORK28_TELEMETRY_EVIDENCE_DIR ?? process.env.AM_WORK28_TELEMETRY_EVIDENCE_DIR ?? join(guiRoot, 'test-results')
      try {
        writeFileSync(join(directory, `harness-stderr-${child.pid}.log`), text)
      } catch {
      }
    }
  }
  let stopPromise: Promise<void> | null = null
  const stop = (): Promise<void> => {
    if (stopPromise != null) {
      return stopPromise
    }
    stopPromise = (async () => {
      lines.close()
      pipe.destroy()
      if (!exited) {
        const done = await Promise.race([exitPromise.then(() => true), new Promise<boolean>((r) => setTimeout(() => r(false), 10_000))])
        if (!done) {
          try {
            process.kill(-child.pid!, 'SIGKILL')
          } catch {
            child.kill('SIGKILL')
          }
          await exitPromise
        }
      }
      if (!closed) {
        await closePromise
      }
      dumpStderr()
      if (exitCode !== 0) {
        throw new Error(`Local API harness failed (exit ${exitCode ?? 'signal'}, signal ${exitSignal ?? 'none'}); see retained harness log.`)
      }
    })()
    return stopPromise
  }
  const bootstrap = await new Promise<Bootstrap>((resolvePromise, reject) => {
    const timer = setTimeout(() => {
      const reason = 'Local API harness bootstrap timed out'
      void stop().then(
        () => reject(new Error(`${reason}; see retained harness log.`)),
        () => reject(new Error(`${reason} (exit ${exitCode ?? 'signal'}, signal ${exitSignal ?? 'none'}); see retained harness log.`)),
      )
    }, 60_000)
    lines.once('line', (line) => {
      clearTimeout(timer)
      try {
        resolvePromise(JSON.parse(line) as Bootstrap)
      } catch {
        const reason = 'Local API harness bootstrap was invalid'
        void stop().then(
          () => reject(new Error(`${reason}; see retained harness log.`)),
          () => reject(new Error(`${reason} (exit ${exitCode ?? 'signal'}, signal ${exitSignal ?? 'none'}); see retained harness log.`)),
        )
      }
    })
    exitPromise.then(() => {
      clearTimeout(timer)
      const reason = 'Local API harness exited before bootstrap'
      void stop().then(
        () => reject(new Error(`${reason}; see retained harness log.`)),
        () => reject(new Error(`${reason} (exit ${exitCode ?? 'signal'}, signal ${exitSignal ?? 'none'}); see retained harness log.`)),
      )
    })
  })
  return { bootstrap, stop }
}
