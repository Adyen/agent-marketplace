import { createRequire } from 'node:module'
import { existsSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { resolve } from 'node:path'

const require = createRequire(import.meta.url)
const mode = process.argv[2] || 'test'

function hasPlaywrightDependency() {
  try {
    require.resolve('@playwright/test')
    return true
  } catch {
    return false
  }
}

function skip(message) {
  console.warn(`E2E skipped: ${message}`)
  console.warn('Install the approved exact-pinned @playwright/test dependency and Chromium before running pnpm run test:e2e.')
  process.exit(0)
}

if (!hasPlaywrightDependency()) skip('the Playwright test dependency is not installed under the active pnpm policy.')

const playwright = createRequire(require.resolve('@playwright/test'))('playwright')
const chromiumPath = playwright.chromium.executablePath()

if (mode === 'install') {
  const result = spawnSync('pnpm', ['exec', 'playwright', 'install', 'chromium'], { stdio: 'inherit' })
  if (result.error || result.status !== 0) skip('Chromium could not be installed without bypassing the active package policy.')
  process.exit(0)
}

if (!existsSync(chromiumPath)) skip(`the Chromium binary is unavailable at ${chromiumPath}.`)
if (!existsSync(resolve('.output/server/index.mjs'))) {
  const build = spawnSync('pnpm', ['run', 'build'], { stdio: 'inherit' })
  if (build.error || build.status !== 0) process.exit(build.status || 1)
}

const result = spawnSync('pnpm', ['exec', 'playwright', 'test', '--config=tests/e2e/playwright.config.ts', ...process.argv.slice(3)], { stdio: 'inherit' })
if (result.error) throw result.error
process.exit(result.status ?? 1)
