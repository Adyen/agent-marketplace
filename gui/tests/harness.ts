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
  orgConfig?: string
  marketplaceEndpoint?: string
  gitSourceURL?: string
  syncStarted?: string
  syncRelease?: string
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
  const exitPromise = new Promise<void>((resolvePromise) =>
    child.once('exit', () => {
      exited = true
      resolvePromise()
    }),
  )
  const stderrText = () => Buffer.concat(stderrChunks).toString('utf8')
  const dumpStderr = () => {
    const text = stderrText()
    if (text.trim() !== '') {
      const directory = process.env.AM_WORK28_TELEMETRY_EVIDENCE_DIR ?? join(guiRoot, 'test-results')
      try {
        writeFileSync(join(directory, `harness-stderr-${child.pid}.log`), text)
      } catch {
      }
    }
  }
  const stop = async () => {
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
    dumpStderr()
  }
  const bootstrap = await new Promise<Bootstrap>((resolvePromise, reject) => {
    const timer = setTimeout(() => {
      void stop().then(() => reject(new Error(`bootstrap timeout: ${stderrText()}`)))
    }, 60_000)
    lines.once('line', (line) => {
      clearTimeout(timer)
      try {
        resolvePromise(JSON.parse(line) as Bootstrap)
      } catch (error) {
        void stop().then(() => reject(error))
      }
    })
    exitPromise.then(() => {
      clearTimeout(timer)
      void stop().then(() => reject(new Error(`harness exited: ${stderrText()}`)))
    })
  })
  return { bootstrap, stop }
}
